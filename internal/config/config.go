package config

import (
	"flag"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Host           string
	Port           int
	Listen         string
	DTUSerial      uint64
	InverterSerial int64
	PollInterval   time.Duration
	Timeout        time.Duration
	EncRandHex     string
	Encrypted      bool

	// MQTT / Home Assistant discovery
	MQTTBroker   string
	MQTTUsername string
	MQTTPassword string
	MQTTTopic    string
	MQTTDiscPref string
	MQTTClientID string
	DeviceName   string
}

func Parse() Config {
	cfg := Config{}
	flag.StringVar(&cfg.Host, "host", env("HOYMILES_HOST", ""), "DTU host")
	flag.IntVar(&cfg.Port, "port", envInt("HOYMILES_PORT", 10081), "DTU port")
	flag.StringVar(&cfg.Listen, "listen", env("HOYMILES_LISTEN", ":8090"), "HTTP listen address")
	flag.Uint64Var(&cfg.DTUSerial, "dtu-serial", uint64(envInt64("HOYMILES_DTU_SERIAL", 0)), "DTU serial")
	flag.Int64Var(&cfg.InverterSerial, "inverter-serial", envInt64("HOYMILES_INV_SERIAL", 0), "Inverter serial")
	flag.DurationVar(&cfg.PollInterval, "poll", envDuration("HOYMILES_POLL", 5*time.Second), "poll interval (min 2s)")
	flag.DurationVar(&cfg.Timeout, "timeout", envDuration("HOYMILES_TIMEOUT", 12*time.Second), "per-request timeout")
	flag.StringVar(&cfg.EncRandHex, "enc-rand", env("HOYMILES_ENC_RAND", ""), "16-byte enc_rand hex (if DTU encrypted)")
	flag.BoolVar(&cfg.Encrypted, "encrypted", env("HOYMILES_ENCRYPTED", "") == "1", "enable AES-GCM")

	flag.StringVar(&cfg.MQTTBroker, "mqtt", env("HOYMILES_MQTT", ""), "MQTT broker URL (e.g. tcp://mqtt-host:1883)")
	flag.StringVar(&cfg.MQTTUsername, "mqtt-user", env("HOYMILES_MQTT_USER", ""), "MQTT username")
	flag.StringVar(&cfg.MQTTPassword, "mqtt-pass", env("HOYMILES_MQTT_PASS", ""), "MQTT password")
	flag.StringVar(&cfg.MQTTTopic, "mqtt-topic", env("HOYMILES_MQTT_TOPIC", "hoymiles/has"), "MQTT topic prefix")
	flag.StringVar(&cfg.MQTTDiscPref, "mqtt-discovery", env("HOYMILES_MQTT_DISCOVERY", "homeassistant"), "HA discovery prefix")
	flag.StringVar(&cfg.MQTTClientID, "mqtt-client-id", env("HOYMILES_MQTT_CLIENT_ID", "hoymiles-gateway"), "MQTT client id")
	flag.StringVar(&cfg.DeviceName, "device-name", env("HOYMILES_DEVICE_NAME", "Hoymiles HAS"), "HA device name")

	flag.Parse()

	if cfg.PollInterval < 2*time.Second {
		cfg.PollInterval = 2 * time.Second
	}
	return cfg
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
