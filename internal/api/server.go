package api

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"hoymiles-gateway/internal/dtu"
	"hoymiles-gateway/pb"
)

//go:embed index.html
var indexHTML []byte

// Snapshot is one poll of the hybrid inverter, scaled to real units.
type Snapshot struct {
	Timestamp      int64           `json:"timestamp"`
	InverterSerial int64           `json:"inverter_serial"`
	ActivePower    int32           `json:"active_power"`
	Production     any             `json:"production,omitempty"`
	Consumption    any             `json:"consumption,omitempty"`
	PVPanels       []any           `json:"pv_panels,omitempty"`
	Battery        any             `json:"battery"`
	Grid           any             `json:"grid"`
	Load           any             `json:"load"`
	Inverter       any             `json:"inverter"`
	PowerFlow      any             `json:"power_flow"`
	Raw            json.RawMessage `json:"raw,omitempty"`
}

type Registry struct {
	Inverters []RegInverter `json:"inverters"`
	Meter     any           `json:"meter,omitempty"`
	GenMode   int32         `json:"gen_mode"`
	DFS       string        `json:"dfs"`
	Time      int64         `json:"time"`
}

type RegInverter struct {
	SerialNumber string `json:"serial_number"`
	ModelName    string `json:"model_name"`
	SwMVer       string `json:"sw_m_ver"`
	SwSVer       string `json:"sw_s_ver"`
	SwSysVer     string `json:"sw_sys_ver"`
	BmsCap       int32  `json:"bms_cap"`
	PvNum        int32  `json:"pv_num"`
	Addr         int32  `json:"addr"`
	BmsType      int32  `json:"bms_type"`
}

type Bus struct {
	mu        sync.RWMutex
	last      *Snapshot
	lastRaw   *pb.ESDataReqDTO
	registry  *Registry
	lastNet   any
	lastErr   error
	updatedAt time.Time
	subs      map[chan Snapshot]struct{}
	okCount   int64
	errCount  int64
}

// Metrics is a lightweight health snapshot for MQTT/API.
type Metrics struct {
	OK           int64  `json:"ok_count"`
	Errors       int64  `json:"error_count"`
	LastError    string `json:"last_error,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	SecondsSince int64  `json:"seconds_since_update"`
}

func (b *Bus) Metrics() Metrics {
	b.mu.RLock()
	defer b.mu.RUnlock()
	m := Metrics{OK: b.okCount, Errors: b.errCount}
	if b.lastErr != nil {
		m.LastError = b.lastErr.Error()
	}
	if !b.updatedAt.IsZero() {
		m.UpdatedAt = b.updatedAt.UTC().Format(time.RFC3339)
		m.SecondsSince = int64(time.Since(b.updatedAt).Seconds())
	}
	return m
}

func NewBus() *Bus {
	return &Bus{subs: make(map[chan Snapshot]struct{})}
}

func (b *Bus) Publish(s *Snapshot, raw *pb.ESDataReqDTO) {
	b.mu.Lock()
	b.last = s
	b.lastRaw = raw
	b.updatedAt = time.Now()
	b.lastErr = nil
	b.okCount++
	subs := make([]chan Snapshot, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- *s:
		default: // drop if slow
		}
	}
}

func (b *Bus) SetRegistry(r *Registry) {
	b.mu.Lock()
	b.registry = r
	b.mu.Unlock()
}

func (b *Bus) SetNetwork(n any) {
	b.mu.Lock()
	b.lastNet = n
	b.mu.Unlock()
}

func (b *Bus) SetError(err error) {
	b.mu.Lock()
	b.lastErr = err
	b.errCount++
	b.mu.Unlock()
}

func (b *Bus) Snapshot() (*Snapshot, *Registry, any, error, time.Time) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.last, b.registry, b.lastNet, b.lastErr, b.updatedAt
}

func (b *Bus) Subscribe() (chan Snapshot, func()) {
	ch := make(chan Snapshot, 8)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}
}

// Poller periodically reads ES_DATA / ES_REG / GW_NET.
type Poller struct {
	Client    *dtu.Client
	Bus       *Bus
	DTUSerial uint64
	InvSerial int64
	Interval  time.Duration
}

func (p *Poller) Run(ctx context.Context) {
	// registry + network once at start, then refresh every 5 minutes
	tReg := time.NewTicker(5 * time.Minute)
	defer tReg.Stop()
	p.refreshRegistry(ctx)
	p.refreshNetwork(ctx)
	p.refreshData(ctx)

	t := time.NewTicker(p.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.refreshData(ctx)
		case <-tReg.C:
			p.refreshRegistry(ctx)
			p.refreshNetwork(ctx)
		}
	}
}

func (p *Poller) refreshRegistry(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	reg, err := p.Client.ESRegistry(cctx, p.DTUSerial)
	if err != nil {
		log.Printf("ES_REG: %v", err)
		p.Bus.SetError(err)
		return
	}
	out := &Registry{GenMode: reg.GetGenMode(), DFS: fmt.Sprint(reg.GetDfs()), Time: int64(reg.GetTime())}
	if m := reg.GetMeter(); m != nil {
		out.Meter = m
	}
	for _, inv := range reg.GetInverters() {
		out.Inverters = append(out.Inverters, RegInverter{
			SerialNumber: fmt.Sprint(inv.GetSerialNumber()),
			ModelName:    inv.GetModelName(),
			SwMVer:       inv.GetSwMVer(),
			SwSVer:       inv.GetSwSVer(),
			SwSysVer:     inv.GetSwSysVer(),
			BmsCap:       inv.GetBmsCap(),
			PvNum:        inv.GetPvNum(),
			Addr:         inv.GetAddr(),
			BmsType:      inv.GetBmsType(),
		})
	}
	p.Bus.SetRegistry(out)
}

func (p *Poller) refreshNetwork(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	net, err := p.Client.GWNetwork(cctx, p.DTUSerial)
	if err != nil {
		log.Printf("GW_NET: %v", err)
		return
	}
	p.Bus.SetNetwork(net)
}

func (p *Poller) refreshData(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	data, err := p.Client.ESData(cctx, p.DTUSerial, p.InvSerial)
	if err != nil {
		log.Printf("ES_DATA: %v", err)
		p.Bus.SetError(err)
		return
	}
	snap := scaleSnapshot(data)
	p.Bus.Publish(snap, data)
}

// scaleSnapshot converts raw protobuf ints into real units
// (same conversion factors as ha-hoymiles-wifi).
func scaleSnapshot(d *pb.ESDataReqDTO) *Snapshot {
	s := &Snapshot{
		Timestamp:      int64(d.GetTimestamp()),
		InverterSerial: d.GetSerialNumber(),
		ActivePower:    d.GetActivePower(),
	}

	// Battery
	if b := d.GetBatteryManagement(); b != nil {
		s.Battery = map[string]any{
			"state_of_charge":         b.GetStateOfCharge(),
			"state_of_health":         b.GetStateOfHealth(),
			"voltage":                 f32(b.GetVoltage(), 0.1),
			"current":                 b.GetCurrent(),
			"power":                   b.GetPower(),
			"internal_charge_mode":    f32(b.GetInternalChargeMode(), 0.1),
			"internal_discharge_mode": f32(b.GetInternalDischargeMode(), 0.1),
			"cell_voltage_high":       f32(b.GetCellVoltageHigh(), 0.01),
			"cell_voltage_low":        f32(b.GetCellVoltageLow(), 0.01),
			"temp_high_charge":        f32(b.GetTempHighCharge(), 0.1),
			"temp_low_charge":         f32(b.GetTempLowCharge(), 0.1),
			"energy_charged":          f32(b.GetEnergyCharged(), 0.1),
			"energy_discharged":       f32(b.GetEnergyDischarged(), 0.1),
			"voltage_charge_high":     f32(b.GetVoltageChargeHigh(), 0.001),
			"voltage_charge_low":      f32(b.GetVoltageChargeLow(), 0.001),
		}
	}

	// Grid / Load / Inverter phases
	s.Grid = scaleGrid(d.GetGrid())
	s.Load = scaleLoad(d.GetLoad())
	s.Inverter = scaleInv(d.GetInverter())

	if pf := d.GetPowerFlow(); pf != nil {
		s.PowerFlow = map[string]any{
			"pv_to_load":      pf.GetPvToLoad(),
			"pv_to_battery":   pf.GetPvToBattery(),
			"pv_to_grid":      pf.GetPvToGrid(),
			"battery_to_load": pf.GetBatteryToLoad(),
			"grid_to_load":    pf.GetGridToLoad(),
			"battery_to_grid": pf.GetBatteryToGrid(),
			"state_of_charge": pf.GetStateOfCharge(),
		}
	}

	if prod := d.GetProduction(); prod != nil {
		s.Production = map[string]any{
			"energy_to_load":    f32(prod.GetEnergyToLoad(), 0.1),
			"energy_to_battery": f32(prod.GetEnergyToBattery(), 0.1),
			"energy_to_grid":    f32(prod.GetEnergyToGrid(), 0.1),
		}
	}
	if cons := d.GetConsumption(); cons != nil {
		s.Consumption = map[string]any{
			"energy_from_pv":      f32(cons.GetEnergyFromPv(), 0.1),
			"energy_from_battery": f32(cons.GetEnergyFromBattery(), 0.1),
			"energy_from_grid":    f32(cons.GetEnergyFromGrid(), 0.1),
		}
	}

	// PV panels (scaled)
	var panels []any
	for _, p := range d.GetPvPanels() {
		panels = append(panels, map[string]any{
			"solar_irradiance": p.GetSolarIrradiance(),
			"voltage":          f32(p.GetVoltage(), 0.1),
			"current":          f32(p.GetCurrent(), 0.01),
			"power":            p.GetPower(),
			"energy":           f32(p.GetEnergy(), 0.1),
		})
	}
	s.PVPanels = panels
	return s
}

func f32(v int32, scale float64) float64 {
	return float64(v) * scale
}

func scaleGrid(g *pb.DGridMO) any {
	if g == nil {
		return nil
	}
	out := map[string]any{
		"status":                 g.GetParam().GetStatus(),
		"frequency":              f32(g.GetParam().GetFrequency(), 0.01),
		"power_factor_deviation": g.GetParam().GetPowerFactorDeviation(),
	}
	var phases []any
	for _, p := range g.GetPhases() {
		phases = append(phases, map[string]any{
			"phase":            p.GetPhase(),
			"voltage":          f32(p.GetVoltage(), 0.1),
			"current":          f32(p.GetCurrent(), 0.01),
			"active_power":     p.GetActivePower(),
			"reactive_power":   p.GetReactivePower(),
			"power_factor":     f32(p.GetPowerFactor(), 0.01),
			"energy_frequency": f32(p.GetEnergyFrequency(), 0.1),
			"energy_consumed":  f32(p.GetEnergyConsumed(), 0.1),
		})
	}
	out["phases"] = phases
	return out
}

func scaleLoad(l *pb.DLoadMO) any {
	if l == nil {
		return nil
	}
	out := map[string]any{
		"status":    l.GetParam().GetStatus(),
		"frequency": f32(l.GetParam().GetFrequency(), 0.01),
	}
	var phases []any
	for _, p := range l.GetPhases() {
		phases = append(phases, map[string]any{
			"phase":           p.GetPhase(),
			"voltage":         f32(p.GetVoltage(), 0.1),
			"active_power":    p.GetActivePower(),
			"energy_consumed": f32(p.GetEnergyConsumed(), 0.1),
		})
	}
	out["phases"] = phases
	return out
}

func scaleInv(inv *pb.DInvMO) any {
	if inv == nil {
		return nil
	}
	out := map[string]any{
		"status":               inv.GetParam().GetStatus(),
		"frequency":            f32(inv.GetParam().GetFrequency(), 0.01),
		"isolation_resistance": inv.GetParam().GetIsolationResistance(),
		"leakage_current":      inv.GetParam().GetLeakageCurrent(),
		"drm_signal":           inv.GetParam().GetDrmSignal(),
		"anti_islanding_fault": inv.GetParam().GetAntiIslandingFault(),
	}
	var phases []any
	for _, p := range inv.GetPhases() {
		phases = append(phases, map[string]any{
			"phase":          p.GetPhase(),
			"voltage":        f32(p.GetVoltage(), 0.1),
			"current":        f32(p.GetCurrent(), 0.01),
			"active_power":   p.GetActivePower(),
			"reactive_power": p.GetReactivePower(),
			"dc_current":     f32(p.GetDcCurrent(), 0.01),
			"dc_voltage":     f32(p.GetDcVoltage(), 0.1),
			"eps_voltage":    f32(p.GetEpsVoltage(), 0.1),
			"eps_current":    f32(p.GetEpsCurrent(), 0.01),
			"eps_power":      p.GetEpsPower(),
		})
	}
	out["phases"] = phases
	return out
}

// Server is the HTTP fan-out.
type Server struct {
	Bus       *Bus
	Client    *dtu.Client
	DTUSerial uint64
	InvSerial int64
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	// Use classic patterns (no method syntax) so "/" reliably serves the UI.
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/api/v1/state", s.handleState)
	mux.HandleFunc("/api/v1/registry", s.handleRegistry)
	mux.HandleFunc("/api/v1/network", s.handleNetwork)
	mux.HandleFunc("/api/v1/status", s.handleStatus)
	mux.HandleFunc("/api/v1/stream", s.handleSSE)
	mux.HandleFunc("/api/v1/bms/mode", s.handleBMSMode)
	mux.HandleFunc("/api/v1/command", s.handleCommand)
	mux.HandleFunc("/api/v1/command/power-limit", s.handlePowerLimit)
	mux.HandleFunc("/api/v1/grid-profile", s.handleGridProfile)
	mux.HandleFunc("/api/v1/inverter", s.handleInverter)
	mux.HandleFunc("/api/v1/misc/{name}", s.handleMisc)
	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	_, _, _, err, updated := s.Bus.Snapshot()
	m := s.Bus.Metrics()
	status := "ok"
	code := http.StatusOK
	if err != nil && time.Since(updated) > 2*time.Minute {
		status = "degraded"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status":     status,
		"updated_at": updated,
		"error":      errString(err),
		"metrics":    m,
	})
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	snap, _, _, err, updated := s.Bus.Snapshot()
	if snap == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":      errString(err),
			"updated_at": updated,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"state":      snap,
		"updated_at": updated,
	})
}

func (s *Server) handleRegistry(w http.ResponseWriter, r *http.Request) {
	_, reg, _, _, _ := s.Bus.Snapshot()
	if reg == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "no registry yet"})
		return
	}
	writeJSON(w, http.StatusOK, reg)
}


func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	snap, reg, _, _, updated := s.Bus.Snapshot()
	out := map[string]any{"updated_at": updated}
	if reg != nil {
		out["device"] = map[string]any{
			"model":     firstModel(reg),
			"sw_m":      firstSw(reg, "m"),
			"sw_s":      firstSw(reg, "s"),
			"sw_sys":    firstSw(reg, "sys"),
			"bms_cap":   firstBmsCap(reg),
			"pv_num":    firstPvNum(reg),
			"dfs":       reg.DFS,
			"dtu":       s.DTUSerial,
			"inverter":  s.InvSerial,
		}
	}
	if snap != nil {
		out["live"] = snap
	}
	writeJSON(w, http.StatusOK, out)
}

func firstModel(reg *Registry) string {
	if len(reg.Inverters) > 0 {
		return reg.Inverters[0].ModelName
	}
	return ""
}
func firstSw(reg *Registry, kind string) string {
	if len(reg.Inverters) == 0 {
		return ""
	}
	inv := reg.Inverters[0]
	switch kind {
	case "m":
		return inv.SwMVer
	case "s":
		return inv.SwSVer
	default:
		return inv.SwSysVer
	}
}
func firstBmsCap(reg *Registry) int32 {
	if len(reg.Inverters) > 0 {
		return reg.Inverters[0].BmsCap
	}
	return 0
}
func firstPvNum(reg *Registry) int32 {
	if len(reg.Inverters) > 0 {
		return reg.Inverters[0].PvNum
	}
	return 0
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	_, _, net, _, _ := s.Bus.Snapshot()
	writeJSON(w, http.StatusOK, net)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsub := s.Bus.Subscribe()
	defer unsub()

	// send current snapshot immediately
	if snap, _, _, _, _ := s.Bus.Snapshot(); snap != nil {
		b, _ := json.Marshal(snap)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fl.Flush()
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case snap, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(snap)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}

type bmsRequest struct {
	Mode           string `json:"mode"`
	RevSoc         int32  `json:"rev_soc"`
	MaxPower       int32  `json:"max_power"`
	PeakSoc        int32  `json:"peak_soc"`
	PeakMeterPower int32  `json:"peak_meter_power"`
	InverterSerial int64  `json:"inverter_serial,omitempty"`
}

func (s *Server) handleBMSMode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var req bmsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	mode, err := dtu.ParseBMSMode(req.Mode)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	inv := req.InverterSerial
	if inv == 0 {
		inv = s.InvSerial
	}
	// defaults
	rev := req.RevSoc
	maxP := req.MaxPower
	peakSoc := req.PeakSoc
	peakW := req.PeakMeterPower
	if rev == 0 {
		rev = 20
	}
	if maxP == 0 {
		maxP = 80
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	if err := s.Client.SetBMSMode(ctx, s.DTUSerial, inv, mode, rev, maxP, peakSoc, peakW); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"mode":            req.Mode,
		"rev_soc":         rev,
		"max_power":       maxP,
		"inverter_serial": inv,
	})
}
