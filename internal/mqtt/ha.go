// Package mqtt publishes gateway state to MQTT and announces
// Home Assistant MQTT discovery entities.
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Entity is one HA discovery definition.
type Entity struct {
	Component   string // sensor | binary_sensor | number | select | button
	ID          string // unique id suffix
	Name        string
	Unit        string
	DeviceClass string
	StateClass  string
	Icon        string
	ValueTpl    string // value_json template
	// binary_sensor
	PayloadOn  string
	PayloadOff string
}

type Config struct {
	Broker   string // e.g. tcp://mqtt-host:1883
	Username string
	Password string
	// TopicPrefix is the state topic root (default hoymiles/has)
	TopicPrefix string
	// DiscoveryPrefix default homeassistant
	DiscoveryPrefix string
	ClientID        string
	DeviceName      string
	Model           string
	Manufacturer    string
	// Identifiers for the HA device
	Identifiers []string
	// ExpireAfterSec marks entities unavailable if no state update arrives
	// within this window (HA MQTT discovery `expire_after`). Default 120.
	ExpireAfterSec int
}

// CommandFunc executes an MQTT command payload and returns a JSON-able result.
type CommandFunc func(topic string, payload []byte) (any, error)

type Publisher struct {
	cfg Config
	cli mqtt.Client
	cmd CommandFunc

	mu       sync.Mutex
	announced bool
}

func New(cfg Config) *Publisher {
	if cfg.TopicPrefix == "" {
		cfg.TopicPrefix = "hoymiles/has"
	}
	if cfg.DiscoveryPrefix == "" {
		cfg.DiscoveryPrefix = "homeassistant"
	}
	if cfg.ClientID == "" {
		cfg.ClientID = "hoymiles-gateway"
	}
	if cfg.DeviceName == "" {
		cfg.DeviceName = "Hoymiles HAS"
	}
	if cfg.Model == "" {
		cfg.Model = "HAS-3.0LV-EUG1"
	}
	if cfg.Manufacturer == "" {
		cfg.Manufacturer = "Hoymiles"
	}
	if len(cfg.Identifiers) == 0 {
		cfg.Identifiers = []string{"hoymiles_has"}
	}
	if cfg.ExpireAfterSec <= 0 {
		cfg.ExpireAfterSec = 120
	}
	return &Publisher{cfg: cfg}
}

// SetCommandHandler registers the MQTT command callback (called on Connect).
func (p *Publisher) SetCommandHandler(fn CommandFunc) { p.cmd = fn }

// Resubscribe subscribes commands if already connected (OnConnect does this too).
func (p *Publisher) Resubscribe() {
	if p.cli != nil && p.cli.IsConnected() {
		p.subscribeCommands()
	}
}

func (p *Publisher) Connect() error {
	if p.cfg.Broker == "" {
		return fmt.Errorf("mqtt broker not configured")
	}
	opts := mqtt.NewClientOptions().
		AddBroker(p.cfg.Broker).
		SetClientID(p.cfg.ClientID).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetKeepAlive(30 * time.Second).
		SetOrderMatters(false).
		SetOnConnectHandler(func(c mqtt.Client) {
			log.Printf("mqtt connected to %s", p.cfg.Broker)
			p.mu.Lock()
			p.announced = false
			p.mu.Unlock()
			_ = p.announce()
			p.publishAvailability("online")
			p.subscribeCommands()
		}).
		SetConnectionLostHandler(func(c mqtt.Client, err error) {
			log.Printf("mqtt connection lost: %v", err)
		})
	if p.cfg.Username != "" {
		opts.SetUsername(p.cfg.Username)
		opts.SetPassword(p.cfg.Password)
	}
	p.cli = mqtt.NewClient(opts)
	tok := p.cli.Connect()
	if !tok.WaitTimeout(15*time.Second) {
		return fmt.Errorf("mqtt connect timeout")
	}
	if err := tok.Error(); err != nil {
		return err
	}
	return nil
}

func (p *Publisher) Close() {
	if p.cli == nil {
		return
	}
	p.publishAvailability("offline")
	p.cli.Disconnect(500)
}

func (p *Publisher) topic(parts ...string) string {
	return p.cfg.TopicPrefix + "/" + strings.Join(parts, "/")
}

func (p *Publisher) publishAvailability(state string) {
	if p.cli == nil || !p.cli.IsConnected() {
		return
	}
	tok := p.cli.Publish(p.topic("availability"), 0, true, state)
	tok.Wait()
}

// State topics used by discovery templates.
func (p *Publisher) stateTopic() string { return p.topic("state") }
func (p *Publisher) availabilityTopic() string {
	return p.topic("availability")
}

// defaultEntities covers the live HAS telemetry map.
func defaultEntities() []Entity {
	s := []Entity{
		{Component: "sensor", ID: "state_of_charge", Name: "Battery SOC", Unit: "%", DeviceClass: "battery", StateClass: "measurement", ValueTpl: "{{ value_json.battery.state_of_charge }}"},
		{Component: "sensor", ID: "state_of_health", Name: "Battery SOH", Unit: "%", StateClass: "measurement", ValueTpl: "{{ value_json.battery.state_of_health }}"},
		{Component: "sensor", ID: "battery_voltage", Name: "Battery voltage", Unit: "V", DeviceClass: "voltage", StateClass: "measurement", ValueTpl: "{{ value_json.battery.voltage }}"},
		{Component: "sensor", ID: "battery_power", Name: "Battery power", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.battery.power }}"},
		{Component: "sensor", ID: "battery_temp", Name: "Battery temp", Unit: "°C", DeviceClass: "temperature", StateClass: "measurement", ValueTpl: "{{ value_json.battery.temp_high_charge }}"},
		{Component: "sensor", ID: "grid_active_power", Name: "Grid active power", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.grid.phases[0].active_power }}"},
		{Component: "sensor", ID: "grid_voltage", Name: "Grid voltage", Unit: "V", DeviceClass: "voltage", StateClass: "measurement", ValueTpl: "{{ value_json.grid.phases[0].voltage }}"},
		{Component: "sensor", ID: "grid_current", Name: "Grid current", Unit: "A", DeviceClass: "current", StateClass: "measurement", ValueTpl: "{{ value_json.grid.phases[0].current }}"},
		{Component: "sensor", ID: "grid_frequency", Name: "Grid frequency", Unit: "Hz", DeviceClass: "frequency", StateClass: "measurement", ValueTpl: "{{ value_json.grid.frequency }}"},
		{Component: "sensor", ID: "grid_energy", Name: "Grid energy", Unit: "kWh", DeviceClass: "energy", StateClass: "total_increasing", ValueTpl: "{{ value_json.grid.phases[0].energy_consumed }}"},
		{Component: "sensor", ID: "load_active_power", Name: "Load active power", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.load.phases[0].active_power }}"},
		{Component: "sensor", ID: "load_voltage", Name: "Load voltage", Unit: "V", DeviceClass: "voltage", StateClass: "measurement", ValueTpl: "{{ value_json.load.phases[0].voltage }}"},
		{Component: "sensor", ID: "load_energy", Name: "Load energy", Unit: "kWh", DeviceClass: "energy", StateClass: "total_increasing", ValueTpl: "{{ value_json.load.phases[0].energy_consumed }}"},
		{Component: "sensor", ID: "inverter_active_power", Name: "Inverter active power", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.inverter.phases[0].active_power }}"},
		{Component: "sensor", ID: "inverter_voltage", Name: "Inverter voltage", Unit: "V", DeviceClass: "voltage", StateClass: "measurement", ValueTpl: "{{ value_json.inverter.phases[0].voltage }}"},
		{Component: "sensor", ID: "inverter_dc_voltage", Name: "Inverter DC voltage", Unit: "V", DeviceClass: "voltage", StateClass: "measurement", ValueTpl: "{{ value_json.inverter.phases[0].dc_voltage }}"},
		{Component: "sensor", ID: "inverter_dc_current", Name: "Inverter DC current", Unit: "A", DeviceClass: "current", StateClass: "measurement", ValueTpl: "{{ value_json.inverter.phases[0].dc_current }}"},
		{Component: "sensor", ID: "flow_pv_to_load", Name: "PV to load", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.power_flow.pv_to_load }}"},
		{Component: "sensor", ID: "flow_pv_to_battery", Name: "PV to battery", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.power_flow.pv_to_battery }}"},
		{Component: "sensor", ID: "flow_pv_to_grid", Name: "PV to grid", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.power_flow.pv_to_grid }}"},
		{Component: "sensor", ID: "flow_battery_to_load", Name: "Battery to load", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.power_flow.battery_to_load }}"},
		{Component: "sensor", ID: "flow_grid_to_load", Name: "Grid to load", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.power_flow.grid_to_load }}"},
		{Component: "sensor", ID: "flow_battery_to_grid", Name: "Battery to grid", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.power_flow.battery_to_grid }}"},
		{Component: "sensor", ID: "pv1_power", Name: "PV string 1 power", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.pv_panels[0].power }}"},
		{Component: "sensor", ID: "pv2_power", Name: "PV string 2 power", Unit: "W", DeviceClass: "power", StateClass: "measurement", ValueTpl: "{{ value_json.pv_panels[1].power }}"},
		{Component: "sensor", ID: "grid_import", Name: "Grid import", Unit: "W", DeviceClass: "power", StateClass: "measurement", Icon: "mdi:transmission-tower-import",
			ValueTpl: "{% set p = value_json.grid.phases[0].active_power | float(0) %}{{ p | abs if p < 0 else 0 }}"},
		{Component: "sensor", ID: "grid_export", Name: "Grid export", Unit: "W", DeviceClass: "power", StateClass: "measurement", Icon: "mdi:transmission-tower-export",
			ValueTpl: "{% set p = value_json.grid.phases[0].active_power | float(0) %}{{ p | abs if p > 0 else 0 }}"},
		// Energy totals (for HA Energy dashboard)
		{Component: "sensor", ID: "battery_energy_charged", Name: "Battery energy charged", Unit: "kWh", DeviceClass: "energy", StateClass: "total_increasing", ValueTpl: "{{ value_json.battery.energy_charged }}"},
		{Component: "sensor", ID: "battery_energy_discharged", Name: "Battery energy discharged", Unit: "kWh", DeviceClass: "energy", StateClass: "total_increasing", ValueTpl: "{{ value_json.battery.energy_discharged }}"},
		{Component: "sensor", ID: "house_energy", Name: "House energy", Unit: "kWh", DeviceClass: "energy", StateClass: "total_increasing", ValueTpl: "{{ value_json.load.phases[0].energy_consumed }}"},
		{Component: "sensor", ID: "energy_from_grid", Name: "Energy from grid", Unit: "kWh", DeviceClass: "energy", StateClass: "total_increasing", ValueTpl: "{{ value_json.consumption.energy_from_grid }}"},
		{Component: "sensor", ID: "energy_to_grid", Name: "Energy to grid", Unit: "kWh", DeviceClass: "energy", StateClass: "total_increasing", ValueTpl: "{{ value_json.production.energy_to_grid }}"},
	}
	return s
}

func (p *Publisher) deviceBlock() map[string]any {
	return map[string]any{
		"identifiers":  p.cfg.Identifiers,
		"name":         p.cfg.DeviceName,
		"model":        p.cfg.Model,
		"manufacturer": p.cfg.Manufacturer,
		"sw_version":   "hoymiles-gateway",
	}
}

// announce publishes retained discovery configs.
func (p *Publisher) announce() error {
	if p.cli == nil {
		return fmt.Errorf("mqtt not connected")
	}
	p.mu.Lock()
	if p.announced {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	ents := defaultEntities()
	for _, e := range ents {
		topic := fmt.Sprintf("%s/%s/hoymiles_has_%s/config", p.cfg.DiscoveryPrefix, e.Component, e.ID)
		cfg := map[string]any{
			"name":             e.Name,
			"unique_id":        "hoymiles_has_" + e.ID,
			"state_topic":      p.stateTopic(),
			"availability_topic": p.availabilityTopic(),
			"device":           p.deviceBlock(),
			"value_template":   e.ValueTpl,
			"expire_after":     p.cfg.ExpireAfterSec,
		}
		if e.Unit != "" {
			cfg["unit_of_measurement"] = e.Unit
		}
		if e.DeviceClass != "" {
			cfg["device_class"] = e.DeviceClass
		}
		if e.StateClass != "" {
			cfg["state_class"] = e.StateClass
		}
		if e.Icon != "" {
			cfg["icon"] = e.Icon
		}
		b, _ := json.Marshal(cfg)
		tok := p.cli.Publish(topic, 0, true, b)
		tok.Wait()
		if tok.Error() != nil {
			return tok.Error()
		}
	}

	// Live connection marker (no expire_after — availability topic is the state)
	cfg := map[string]any{
		"name":            "Hoymiles gateway online",
		"unique_id":       "hoymiles_has_gateway_online",
		"state_topic":     p.availabilityTopic(),
		"payload_on":      "online",
		"payload_off":     "offline",
		"device_class":    "connectivity",
		"device":          p.deviceBlock(),
		"entity_category": "diagnostic",
	}

	// Health metrics (diagnostic)
	for _, m := range []map[string]any{
		{
			"name":              "Hoymiles DTU errors",
			"unique_id":         "hoymiles_has_dtu_errors",
			"state_topic":       p.topic("metrics"),
			"value_template":    "{{ value_json.error_count }}",
			"availability_topic": p.availabilityTopic(),
			"device":            p.deviceBlock(),
			"entity_category":   "diagnostic",
			"state_class":       "total_increasing",
			"expire_after":      p.cfg.ExpireAfterSec,
			"icon":              "mdi:alert-circle-outline",
		},
		{
			"name":              "Hoymiles DTU ok polls",
			"unique_id":         "hoymiles_has_dtu_ok",
			"state_topic":       p.topic("metrics"),
			"value_template":    "{{ value_json.ok_count }}",
			"availability_topic": p.availabilityTopic(),
			"device":            p.deviceBlock(),
			"entity_category":   "diagnostic",
			"state_class":       "total_increasing",
			"expire_after":      p.cfg.ExpireAfterSec,
			"icon":              "mdi:check-circle-outline",
		},
		{
			"name":              "Hoymiles seconds since update",
			"unique_id":         "hoymiles_has_seconds_since_update",
			"state_topic":       p.topic("metrics"),
			"value_template":    "{{ value_json.seconds_since_update }}",
			"availability_topic": p.availabilityTopic(),
			"device":            p.deviceBlock(),
			"entity_category":   "diagnostic",
			"unit_of_measurement": "s",
			"expire_after":      p.cfg.ExpireAfterSec,
			"icon":              "mdi:timer-outline",
		},
	} {
		bm, _ := json.Marshal(m)
		tk := p.cli.Publish(fmt.Sprintf("%s/sensor/%s/config", p.cfg.DiscoveryPrefix, m["unique_id"]), 0, true, bm)
		tk.Wait()
	}

	// Last successful DTU update (diagnostic)
	lastCfg := map[string]any{
		"name":              "Hoymiles last update",
		"unique_id":         "hoymiles_has_last_update",
		"state_topic":       p.topic("last_update"),
		"availability_topic": p.availabilityTopic(),
		"device_class":      "timestamp",
		"device":            p.deviceBlock(),
		"entity_category":   "diagnostic",
		"expire_after":      p.cfg.ExpireAfterSec,
	}
	lb, _ := json.Marshal(lastCfg)
	ltok := p.cli.Publish(fmt.Sprintf("%s/sensor/hoymiles_has_last_update/config", p.cfg.DiscoveryPrefix), 0, true, lb)
	ltok.Wait()
	b, _ := json.Marshal(cfg)
	tok := p.cli.Publish(fmt.Sprintf("%s/binary_sensor/hoymiles_has_gateway_online/config", p.cfg.DiscoveryPrefix), 0, true, b)
	tok.Wait()

	p.mu.Lock()
	p.announced = true
	p.mu.Unlock()
	log.Printf("mqtt discovery announced (%d sensors + last_update) under %s expire_after=%ds",
		len(ents), p.cfg.DiscoveryPrefix, p.cfg.ExpireAfterSec)
	return nil
}

func (p *Publisher) subscribeCommands() {
	if p.cmd == nil || p.cli == nil {
		return
	}
	topic := p.topic("cmd")
	tok := p.cli.Subscribe(topic, 0, func(_ mqtt.Client, msg mqtt.Message) {
		res, err := p.cmd(msg.Topic(), msg.Payload())
		payload := map[string]any{"ok": err == nil}
		if err != nil {
			payload["error"] = err.Error()
			log.Printf("mqtt cmd error: %v", err)
		}
		if res != nil {
			payload["result"] = res
		}
		b, _ := json.Marshal(payload)
		pt := p.cli.Publish(p.topic("cmd", "result"), 0, false, b)
		pt.Wait()
	})
	tok.Wait()
	if tok.Error() != nil {
		log.Printf("mqtt subscribe cmd: %v", tok.Error())
		return
	}
	log.Printf("mqtt listening for commands on %s", topic)
}

// PublishState writes the scaled snapshot JSON to the state topic and
// refreshes last_update (keeps expire_after happy).
func (p *Publisher) PublishState(snap any) {
	if p.cli == nil || !p.cli.IsConnected() {
		return
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tok := p.cli.Publish(p.stateTopic(), 0, true, b)
	tok.Wait()
	// ISO-8601 timestamp for expire_after / diagnostics
	ts := time.Now().UTC().Format(time.RFC3339)
	tok2 := p.cli.Publish(p.topic("last_update"), 0, true, ts)
	tok2.Wait()
	// keep gateway_online / availability fresh for HA
	p.publishAvailability("online")
}

// PublishRegistry writes registry info (optional, not a discovery entity).
func (p *Publisher) PublishRegistry(reg any) {
	if p.cli == nil || !p.cli.IsConnected() {
		return
	}
	b, err := json.Marshal(reg)
	if err != nil {
		return
	}
	tok := p.cli.Publish(p.topic("registry"), 0, true, b)
	tok.Wait()
}

// PublishMetrics publishes health counters for diagnostics.
func (p *Publisher) PublishMetrics(m any) {
	if p.cli == nil || !p.cli.IsConnected() {
		return
	}
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	tok := p.cli.Publish(p.topic("metrics"), 0, true, b)
	tok.Wait()
}
