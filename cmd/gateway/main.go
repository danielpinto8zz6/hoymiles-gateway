package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"hoymiles-gateway/internal/api"
	"hoymiles-gateway/internal/config"
	"hoymiles-gateway/internal/dtu"
	"hoymiles-gateway/internal/history"
	"hoymiles-gateway/internal/mqtt"
)

func main() {
	cfg := config.Parse()

	client := dtu.New(cfg.Host)
	client.SetTimeout(cfg.Timeout)
	if cfg.Encrypted && cfg.EncRandHex != "" {
		raw, err := hex.DecodeString(cfg.EncRandHex)
		if err != nil || len(raw) != 16 {
			log.Fatalf("invalid --enc-rand: need 32 hex chars")
		}
		client.SetEncryption(raw, true)
	}
	client.SetDTUSerial(cfg.DTUSerial)

	bus := api.NewBus()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// SQLite telemetry history
	var hist *history.Store
	if cfg.DBPath != "" {
		var err error
		hist, err = history.Open(cfg.DBPath)
		if err != nil {
			log.Printf("history disabled: %v", err)
			hist = nil
		} else {
			log.Printf("history store: %s (%s)", cfg.DBPath, hist)
			// persist every snapshot
			go func() {
				ch, unsub := bus.Subscribe()
				defer unsub()
				for snap := range ch {
					_ = hist.Insert(snapshotToSample(snap))
				}
			}()
			// prune daily
			if cfg.HistoryDays > 0 {
				go func() {
					tk := time.NewTicker(24 * time.Hour)
					defer tk.Stop()
					for {
						select {
						case <-ctx.Done():
							return
						case <-tk.C:
							if n, err := hist.Prune(time.Now().AddDate(0, 0, -cfg.HistoryDays).Unix()); err == nil && n > 0 {
								log.Printf("history pruned %d rows", n)
							}
						}
					}
				}()
			}
		}
	}

	// MQTT + Home Assistant discovery (optional)
	var pub *mqtt.Publisher
	if cfg.MQTTBroker != "" {
		// expire_after: 3× poll + 30s slack (min 90s)
		expire := int(cfg.PollInterval.Seconds())*3 + 30
		if expire < 90 {
			expire = 90
		}
		commands := &mqtt.Commands{
			Client:    client,
			DTUSerial: cfg.DTUSerial,
			InvSerial: cfg.InverterSerial,
		}
		pub = mqtt.New(mqtt.Config{
			Broker:          cfg.MQTTBroker,
			Username:        cfg.MQTTUsername,
			Password:        cfg.MQTTPassword,
			TopicPrefix:     cfg.MQTTTopic,
			DiscoveryPrefix: cfg.MQTTDiscPref,
			ClientID:        cfg.MQTTClientID,
			DeviceName:      cfg.DeviceName,
			Model:           "HAS-3.0LV-EUG1",
			Manufacturer:    "Hoymiles",
			Identifiers:     []string{"hoymiles_has_" + strconv.FormatUint(cfg.DTUSerial, 10)},
			ExpireAfterSec:  expire,
		})
		pub.SetCommandHandler(commands.Handler())
		if err := pub.Connect(); err != nil {
			log.Printf("mqtt disabled (connect failed): %v", err)
			pub = nil
		} else {
			go func() {
				ch, unsub := bus.Subscribe()
				defer unsub()
				for snap := range ch {
					pub.PublishState(snap)
				}
			}()
			go func() {
				t := time.NewTicker(10 * time.Second)
				defer t.Stop()
				for range t.C {
					if _, reg, _, _, _ := bus.Snapshot(); reg != nil {
						pub.PublishRegistry(reg)
						return
					}
				}
			}()
			// health metrics every 30s
			go func() {
				t := time.NewTicker(30 * time.Second)
				defer t.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-t.C:
						pub.PublishMetrics(bus.Metrics())
					}
				}
			}()
		}
	} else {
		log.Println("mqtt not configured (use -mqtt tcp://host:1883)")
	}

	poller := &api.Poller{
		Client:    client,
		Bus:       bus,
		DTUSerial: cfg.DTUSerial,
		InvSerial: cfg.InverterSerial,
		Interval:  cfg.PollInterval,
	}

	go poller.Run(ctx)

	srv := &api.Server{
		Bus:       bus,
		Client:    client,
		DTUSerial: cfg.DTUSerial,
		InvSerial: cfg.InverterSerial,
		History:   hist,
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-ctx.Done()
		if pub != nil {
			pub.Close()
		}
		if hist != nil {
			_ = hist.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("hoymiles-gateway listening on %s → dtu %s:%d (dtu_sn=%d inv_sn=%d poll=%s mqtt=%q)",
		cfg.Listen, cfg.Host, cfg.Port, cfg.DTUSerial, cfg.InverterSerial, cfg.PollInterval, cfg.MQTTBroker)

	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Println("shutdown complete")
}


func snapshotToSample(s any) history.Sample {
	// bus Snapshot is a struct; convert via JSON for stable field access
	b, err := json.Marshal(s)
	if err != nil {
		return history.Sample{TS: time.Now().Unix()}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return history.Sample{TS: time.Now().Unix()}
	}
	ts := time.Now().Unix()
	if v, ok := m["timestamp"].(float64); ok && v > 0 {
		ts = int64(v)
	}
	return history.FromSnapshot(ts, m)
}
