package dtu

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	"hoymiles-gateway/internal/protocol"
	"hoymiles-gateway/pb"
)

// Command action codes (from hoymiles_wifi.const).
const (
	ActionDTUReboot             int32 = 1
	ActionMIShutdown            int32 = 7
	ActionMISTart               int32 = 6
	ActionLimitPower            int32 = 8
	ActionRefluxControl         int32 = 9
	ActionCTSet                 int32 = 11
	ActionMILock                int32 = 12
	ActionMIUnlock              int32 = 13
	ActionSetGridFile           int32 = 14
	ActionSetWiFiPass           int32 = 19
	ActionPerformanceDataMode   int32 = 33
	ActionSet485Port            int32 = 36
	ActionThreeBalanceSet       int32 = 37
	ActionMIGridProtectSelf     int32 = 38
	ActionSunSpecConfig         int32 = 39
	ActionPowerGenerationCorr   int32 = 40
	ActionGridFileRead          int32 = 41
	ActionCleanWarn             int32 = 42
	ActionDRMSetting            int32 = 43
	ActionLimitPowerPF          int32 = 47
	ActionLimitPowerReactive    int32 = 48
	ActionMeterReverse          int32 = 49
	ActionAlarmList             int32 = 50
	ActionGWReboot              int32 = 4096
	ActionGWReset               int32 = 4097
	ActionGWStopRun             int32 = 4098
	ActionGWCollectRealData     int32 = 4099
	ActionGWCollectVer          int32 = 4100
	ActionGWAutoNetworking      int32 = 4101
	ActionGWUpgrade             int32 = 4102
	ActionINVBootUp             int32 = 8193
	ActionINVShutdown           int32 = 8194
	ActionINVReboot             int32 = 8195
	ActionINVReset              int32 = 8196
	ActionINVCleanWarn          int32 = 8197
	ActionINVCleanHisData       int32 = 8198
	ActionINVUploadRealData     int32 = 8199
	ActionINVFindDev            int32 = 8200
	ActionBMSBalance            int32 = 8208
	ActionBMSReboot             int32 = 8224
	ActionBMSUrgentCharging     int32 = 8225
)

// Dev kinds.
const (
	DevDTU     int32 = 1
	DevMicro   int32 = 3
	DevMeter   int32 = 5
	DevINV     int32 = 6
	DevGateway int32 = 9
	DevBMS     int32 = 10
)

// CommandResult is a decoded CommandPB response (or a note if empty).
type CommandResult struct {
	Action     int32         `json:"action"`
	TID        int64         `json:"tid"`
	ErrCode    int32         `json:"err_code"`
	DtuSN      string        `json:"dtu_sn,omitempty"`
	PackageNow int32         `json:"package_now,omitempty"`
	Raw        proto.Message `json:"-"`
	Empty      bool          `json:"empty"`
	Note       string        `json:"note,omitempty"`
}

// SendCommand issues a CommandPB (tag 0xA305) and parses the reply.
// data is the free-form action payload (often "A:x,B:y,C:z\r" or a config blob).
func (c *Client) SendCommand(ctx context.Context, action, devKind, packageNub int32, data string, miSNs []int64, esSNs []string) (*CommandResult, error) {
	req := &pb.CommandResDTO{
		Time:      nowTS(),
		Action:    action,
		DevKind:   devKind,
		PackageNub: packageNub,
		Tid:       time.Now().UnixMilli() / 1000,
		Data:      data,
		MiToSn:    miSNs,
		EsToSn:    esSNs,
	}
	if req.PackageNub <= 0 {
		req.PackageNub = 1
	}

	var resp pb.CommandReqDTO
	// Commands may be ignored by some hybrid firmwares; keep timeout tight
	// so the HTTP API stays responsive.
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	err := c.roundTrip(cctx, protocol.CmdCommand, req, false, 0, 0, &resp)
	if err != nil {
		return nil, err
	}
	res := &CommandResult{
		Action:     resp.GetAction(),
		TID:        resp.GetTid(),
		ErrCode:    resp.GetErrCode(),
		DtuSN:      resp.GetDtuSn(),
		PackageNow: resp.GetPackageNow(),
		Raw:        &resp,
	}
	if proto.Size(&resp) == 0 {
		res.Empty = true
		res.Note = "DTU accepted connection but returned empty CommandReqDTO (common on some hybrid firmwares)"
	}
	return res, nil
}

// SetPowerLimit sets export power limit percent 0–100 (appended as A:p,B:0,C:0).
// WARNING: rapid writes can wear EEPROM — avoid zero-feed-in loops.
func (c *Client) SetPowerLimit(ctx context.Context, percent int, miSN int64) (*CommandResult, error) {
	if percent < 0 || percent > 100 {
		return nil, fmt.Errorf("percent out of range")
	}
	payload := fmt.Sprintf("A:%d,B:0,C:0\r", percent*10)
	var mi []int64
	if miSN != 0 {
		mi = []int64{miSN}
	}
	return c.SendCommand(ctx, ActionLimitPower, DevDTU, 1, payload, mi, nil)
}

// SetRefluxControl sets export/reflux control.
// on=true enables limit mode; data is vendor-specific (often a threshold string).
func (c *Client) SetRefluxControl(ctx context.Context, data string, miSN int64) (*CommandResult, error) {
	var mi []int64
	if miSN != 0 {
		mi = []int64{miSN}
	}
	return c.SendCommand(ctx, ActionRefluxControl, DevDTU, 1, data, mi, nil)
}

// ReadGridFile requests the active grid-code profile (action 41).
func (c *Client) ReadGridFile(ctx context.Context) (*CommandResult, error) {
	return c.SendCommand(ctx, ActionGridFileRead, DevDTU, 1, "", nil, nil)
}

// ParseGridFile attempts to decode a CommandPB data payload as GridFileDataDTO.
// The app stores this protobuf on SET_GRID_FILE / GRID_FILE_READ paths.
func ParseGridFile(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty grid file data")
	}
	var gf pb.GridFileDataDTO
	if err := proto.Unmarshal(data, &gf); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if r := gf.GetRule(); r != nil {
		out["rule"] = map[string]any{
			"rid":     r.GetRid(),
			"grid_id": r.GetGridId(),
			"min_ver": r.GetMinVer(),
			"max_ver": r.GetMaxVer(),
			"prefix":  r.GetPrefix(),
		}
	}
	if cfg := gf.GetCfg(); cfg != nil {
		var items []any
		for _, it := range cfg.GetList() {
			items = append(items, map[string]any{
				"key":    it.GetKey(),
				"name":   it.GetName(),
				"unit":   it.GetUnit(),
				"min":    it.GetMin(),
				"max":    it.GetMax(),
				"edit":   it.GetEdit(),
				"scale":  it.GetScale(),
				"content": it.GetContent(),
			})
		}
		out["cfg"] = map[string]any{
			"id":            cfg.GetId(),
			"ver":           cfg.GetVer(),
			"name":          cfg.GetName(),
			"country_std":   cfg.GetCountryStd(),
			"country_code":  cfg.GetCountryCode(),
			"items":         items,
		}
	}
	return out, nil
}

// WriteGridFile pushes a grid-code profile blob (action 14).
// The payload format is firmware-specific; pass the raw cfg_data string from
// a prior read/export. SAFETY: wrong values can violate local grid codes.
func (c *Client) WriteGridFile(ctx context.Context, data string) (*CommandResult, error) {
	if data == "" {
		return nil, fmt.Errorf("empty grid file data")
	}
	return c.SendCommand(ctx, ActionSetGridFile, DevDTU, 1, data, nil, nil)
}

// SetDRM configures DRM (demand response) — data is vendor string.
func (c *Client) SetDRM(ctx context.Context, data string) (*CommandResult, error) {
	return c.SendCommand(ctx, ActionDRMSetting, DevDTU, 1, data, nil, nil)
}

// SetMeterReverse toggles CT/meter reverse direction (action 49).
func (c *Client) SetMeterReverse(ctx context.Context, data string) (*CommandResult, error) {
	return c.SendCommand(ctx, ActionMeterReverse, DevDTU, 1, data, nil, nil)
}

// SetCT configures CT meter (action 11).
func (c *Client) SetCT(ctx context.Context, data string) (*CommandResult, error) {
	return c.SendCommand(ctx, ActionCTSet, DevDTU, 1, data, nil, nil)
}

// AlarmList requests alarm/event list (action 50).
func (c *Client) AlarmList(ctx context.Context) (*CommandResult, error) {
	return c.SendCommand(ctx, ActionAlarmList, 0, 1, "", nil, nil)
}

// InverterPower controls hybrid inverter power state.
func (c *Client) InverterPower(ctx context.Context, on bool, invSN int64) (*CommandResult, error) {
	action := ActionINVShutdown
	if on {
		action = ActionINVBootUp
	}
	return c.SendCommand(ctx, action, DevINV, 1, "", []int64{invSN}, nil)
}

// InverterReboot reboots the hybrid inverter.
func (c *Client) InverterReboot(ctx context.Context, invSN int64) (*CommandResult, error) {
	return c.SendCommand(ctx, ActionINVReboot, DevINV, 1, "", []int64{invSN}, nil)
}

// EnablePerformanceDataMode unlocks higher-rate telemetry (re-enable after DTU reboot).
func (c *Client) EnablePerformanceDataMode(ctx context.Context) (*CommandResult, error) {
	return c.SendCommand(ctx, ActionPerformanceDataMode, DevDTU, 1, "", nil, nil)
}

// DevConfigFetch pulls a config rule blob (rule_type / rule_id identify grid profile).
func (c *Client) DevConfigFetch(ctx context.Context, dtuSN, devSN string, ruleID, ruleType int32) (*pb.DevConfigFetchReqDTO, error) {
	req := &pb.DevConfigFetchResDTO{
		ResponseTime:   nowTS(),
		TransactionId: time.Now().UnixMilli(),
		DtuSn:          dtuSN,
		DevSn:          devSN,
		CurrentPackage: 0,
		RuleType:       ruleType,
	}
	// request body is the ResDTO; response parsed as ReqDTO (naming follows Python lib)
	var resp pb.DevConfigFetchReqDTO
	// CMD_DEV_CONFIG_FETCH_RES = 0xA307
	if err := c.roundTrip(ctx, protocol.CmdDevConfigFetch, req, false, 0, 0, &resp); err != nil {
		return nil, err
	}
	// stamp requested rule id if device omitted it
	if resp.RuleId == 0 {
		resp.RuleId = ruleID
	}
	return &resp, nil
}

// DevConfigPut writes a config rule blob (grid profile).
func (c *Client) DevConfigPut(ctx context.Context, dtuSN, devSN string, ruleID, ruleType int32, cfgData string, cfgCRC int32, miSNs []int64) (*pb.DevConfigPutReqDTO, error) {
	req := &pb.DevConfigPutResDTO{
		ResponseTime:   nowTS(),
		TransactionId: time.Now().UnixMilli(),
		RuleId:         ruleID,
		Data:           cfgData,
		Crc:            cfgCRC,
		DtuSn:          dtuSN,
		DevSn:          devSN,
		CfgData:        cfgData,
		CfgCrc:         cfgCRC,
		TotalPackages:  1,
		CurrentPackage: 1,
		MiToSn:         miSNs,
		RuleType:       ruleType,
	}
	var resp pb.DevConfigPutReqDTO
	if err := c.roundTrip(ctx, protocol.CmdDevConfigPut, req, false, 0, 0, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GridProfileCode returns pv_grid_profile_code / pv_grid_profile from APP info (best-effort).
func (c *Client) AppInfo(ctx context.Context) (*pb.APPInfoDataReqDTO, error) {
	req := &pb.APPInfoDataResDTO{
		TimeYmdHms: nowHMS(),
		Offset:     Offset,
		Time:       uint32(nowTS()),
	}
	var resp pb.APPInfoDataReqDTO
	if err := c.roundTrip(ctx, protocol.CmdAppInfoDataRes, req, false, 0, 0, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
