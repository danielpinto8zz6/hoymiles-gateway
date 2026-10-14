package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"hoymiles-gateway/internal/dtu"
)

// --- command / control handlers ---

type commandRequest struct {
	Action    int32   `json:"action"`
	DevKind   int32   `json:"dev_kind"`
	Data      string  `json:"data"`
	MiSNs     []int64 `json:"mi_to_sn,omitempty"`
	EsSNs     []string `json:"es_to_sn,omitempty"`
	Confirm   string  `json:"confirm,omitempty"`
	// convenience
	Percent   int   `json:"percent,omitempty"`
	MiSN      int64 `json:"mi_sn,omitempty"`
}

// dangerousActions require explicit confirm phrase.
var dangerousActions = map[int32]bool{
	dtu.ActionSetGridFile:         true,
	dtu.ActionMIGridProtectSelf:   true,
	dtu.ActionSetWiFiPass:         true,
	dtu.ActionGWReset:             true,
	dtu.ActionGWStopRun:           true,
	dtu.ActionINVShutdown:         true,
	dtu.ActionINVReset:            true,
	dtu.ActionINVCleanHisData:     true,
	dtu.ActionMeterReverse:        true,
	dtu.ActionCTSet:               true,
	dtu.ActionThreeBalanceSet:     true,
	dtu.ActionPowerGenerationCorr: true,
	dtu.ActionBMSUrgentCharging:   true,
}

const confirmPhrase = "I_UNDERSTAND"

func (s *Server) requireConfirm(w http.ResponseWriter, action int32, confirm string) bool {
	if !dangerousActions[action] {
		return true
	}
	if confirm == confirmPhrase {
		return true
	}
	writeJSON(w, http.StatusPreconditionFailed, map[string]any{
		"error":           "dangerous action requires confirm",
		"confirm_phrase":  confirmPhrase,
		"action":          action,
		"warning":         "Grid-code / meter / protection changes can violate local regulations or damage equipment. Export current profile first.",
	})
	return false
}

// POST /api/v1/command  — generic CommandPB passthrough
func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Action == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "action required"})
		return
	}
	if !s.requireConfirm(w, req.Action, req.Confirm) {
		return
	}

	ctx := r.Context()
	res, err := s.Client.SendCommand(ctx, req.Action, req.DevKind, 1, req.Data, req.MiSNs, req.EsSNs)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /api/v1/command/power-limit  {"percent":80,"mi_sn":...}
func (s *Server) handlePowerLimit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var req commandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	mi := req.MiSN
	if mi == 0 {
		mi = s.InvSerial
	}
	res, err := s.Client.SetPowerLimit(r.Context(), req.Percent, mi)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// GET /api/v1/grid-profile — best-effort read (command 41 + app info)
// POST /api/v1/grid-profile — write profile (DANGEROUS)
func (s *Server) handleGridProfile(w http.ResponseWriter, r *http.Request) {
	// Grid profile is READ-ONLY on this site (safety / EN 50549-1).
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": "grid profile is read-only; write disabled",
		})
		return
	}
	s.handleGridProfileRead(w, r)
}

func (s *Server) handleGridProfileRead(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"note": "Hybrid HAS firmwares may not answer CommandPB (0xA305). Empty/timeout is expected until the payload format is confirmed.",
	}

	// Prefer a short timeout; do not chain three 12s calls.
	type resOrErr struct {
		name string
		val  any
		err  error
	}

	res, err := s.Client.ReadGridFile(r.Context())
	if err != nil {
		out["grid_file_read_error"] = err.Error()
	} else {
		out["grid_file_read"] = res
	}

	// Only try APP info / DevConfig if the first call was quick-ish
	if _, hasErr := out["grid_file_read_error"]; !hasErr {
		if app, err := s.Client.AppInfo(r.Context()); err == nil && app != nil {
			var pv []any
			for _, p := range app.GetPvInfo() {
				pv = append(pv, map[string]any{
					"pv_serial_number":     p.GetPvSerialNumber(),
					"pv_grid_profile":      p.GetPvGridProfile(),
					"pv_grid_profile_code": p.GetPvGridProfileCode(),
					"mi_rule_id":           p.GetMiRuleId(),
				})
			}
			out["app_info"] = map[string]any{
				"dtu_serial": app.GetDtuSerialNumber(),
				"pv_info":    pv,
			}
		} else if err != nil {
			out["app_info_error"] = err.Error()
		}
	} else {
		out["skipped"] = []string{"app_info", "dev_config"}
	}

	writeJSON(w, http.StatusOK, out)
}

type gridWriteRequest struct {
	CfgData string `json:"cfg_data"`
	CfgCRC  int32  `json:"cfg_crc"`
	RuleID  int32  `json:"rule_id"`
	RuleType int32 `json:"rule_type"`
	Confirm string `json:"confirm"`
	// also allow raw action-14 blob
	Raw string `json:"raw,omitempty"`
}

// POST /api/v1/grid-profile — write profile (DANGEROUS)
func (s *Server) handleGridProfileWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var req gridWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Confirm != confirmPhrase {
		writeJSON(w, http.StatusPreconditionFailed, map[string]any{
			"error":          "grid profile write requires confirm",
			"confirm_phrase": confirmPhrase,
			"warning":        "Incorrect grid-code values can violate local regulations, trip protections, or damage equipment. Always export/backup the current profile first.",
		})
		return
	}

	out := map[string]any{}
	if req.Raw != "" {
		res, err := s.Client.WriteGridFile(r.Context(), req.Raw)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
			return
		}
		out["set_grid_file"] = res
	}
	if req.CfgData != "" {
		res, err := s.Client.DevConfigPut(
			r.Context(),
			strconv.FormatUint(s.DTUSerial, 10),
			strconv.FormatInt(s.InvSerial, 10),
			req.RuleID, req.RuleType, req.CfgData, req.CfgCRC, nil,
		)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "partial": out})
			return
		}
		out["dev_config_put"] = res
	}
	if len(out) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "cfg_data or raw required"})
		return
	}
	out["ok"] = true
	writeJSON(w, http.StatusOK, out)
}

// POST /api/v1/inverter {"on":true} or {"reboot":true}
func (s *Server) handleInverter(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "POST required"})
		return
	}
	var req struct {
		On     bool   `json:"on"`
		Reboot bool   `json:"reboot"`
		Confirm string `json:"confirm"`
		MiSN   int64  `json:"mi_sn"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if req.Reboot || !req.On {
		if req.Confirm != confirmPhrase {
			writeJSON(w, http.StatusPreconditionFailed, map[string]any{
				"error":          "reboot/shutdown requires confirm",
				"confirm_phrase": confirmPhrase,
			})
			return
		}
	}
	sn := req.MiSN
	if sn == 0 {
		sn = s.InvSerial
	}
	var res *dtu.CommandResult
	var err error
	switch {
	case req.Reboot:
		res, err = s.Client.InverterReboot(r.Context(), sn)
	default:
		res, err = s.Client.InverterPower(r.Context(), req.On, sn)
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// POST /api/v1/misc/alarms | /api/v1/misc/ct | /api/v1/misc/drm | /api/v1/misc/meter-reverse
func (s *Server) handleMisc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "GET/POST required"})
		return
	}
	var req struct {
		Data    string `json:"data"`
		Confirm string `json:"confirm"`
	}
	if r.Method == http.MethodPost {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	var res *dtu.CommandResult
	var err error
	switch r.PathValue("name") {
	case "alarms":
		res, err = s.Client.AlarmList(r.Context())
	case "performance":
		res, err = s.Client.EnablePerformanceDataMode(r.Context())
	case "ct":
		if !s.requireConfirm(w, dtu.ActionCTSet, req.Confirm) {
			return
		}
		res, err = s.Client.SetCT(r.Context(), req.Data)
	case "drm":
		res, err = s.Client.SetDRM(r.Context(), req.Data)
	case "meter-reverse":
		if !s.requireConfirm(w, dtu.ActionMeterReverse, req.Confirm) {
			return
		}
		res, err = s.Client.SetMeterReverse(r.Context(), req.Data)
	case "reflux":
		res, err = s.Client.SetRefluxControl(r.Context(), req.Data, s.InvSerial)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "unknown misc action"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}
