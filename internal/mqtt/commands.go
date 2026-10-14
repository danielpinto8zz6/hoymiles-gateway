package mqtt

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"hoymiles-gateway/internal/dtu"
)

// CommandRequest is the JSON body published to <prefix>/cmd.
//
//	{"cmd":"bms_mode","mode":"self_use","rev_soc":20,"max_power":80}
//	{"cmd":"power_limit","percent":80}
//	{"cmd":"inverter","on":true}
//	{"cmd":"inverter","reboot":true}
//	{"cmd":"ping"}
type CommandRequest struct {
	Cmd string `json:"cmd"`

	// bms_mode
	Mode           string `json:"mode"`
	RevSoc         int32  `json:"rev_soc"`
	MaxPower       int32  `json:"max_power"`
	PeakSoc        int32  `json:"peak_soc"`
	PeakMeterPower int32  `json:"peak_meter_power"`

	// power_limit / export
	Percent int    `json:"percent"`
	Data    string `json:"data"`

	// inverter
	On     bool `json:"on"`
	Reboot bool `json:"reboot"`

	// optional target
	InverterSerial int64 `json:"inverter_serial"`
}

// Commands wires MQTT commands to the DTU client.
type Commands struct {
	Client    *dtu.Client
	DTUSerial uint64
	InvSerial int64
}

// Handler returns a CommandFunc for Publisher.SetCommandHandler.
func (c *Commands) Handler() CommandFunc {
	return func(_ string, payload []byte) (any, error) {
		var req CommandRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, fmt.Errorf("bad json: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()

		inv := req.InverterSerial
		if inv == 0 {
			inv = c.InvSerial
		}

		switch req.Cmd {
		case "ping", "":
			return map[string]any{"pong": true}, nil

		case "bms_mode", "mode":
			mode, err := dtu.ParseBMSMode(req.Mode)
			if err != nil {
				return nil, err
			}
			rev := req.RevSoc
			if rev == 0 {
				rev = 20
			}
			maxP := req.MaxPower
			if maxP == 0 {
				maxP = 80
			}
			err = c.Client.SetBMSMode(ctx, c.DTUSerial, inv, mode, rev, maxP, req.PeakSoc, req.PeakMeterPower)
			return map[string]any{
				"mode":      req.Mode,
				"rev_soc":   rev,
				"max_power": maxP,
			}, err

		case "power_limit", "limit":
			res, err := c.Client.SetPowerLimit(ctx, req.Percent, inv)
			return res, err

		case "export_limit", "reflux":
			// Export management / feed-in limit (app Toolkit).
			// data string is firmware-specific; default A:pct*10.
			payload := req.Data
			if payload == "" {
				payload = fmt.Sprintf("A:%d,B:0,C:0\r", req.Percent*10)
			}
			res, err := c.Client.SetRefluxControl(ctx, payload, inv)
			return res, err

		case "drm":
			res, err := c.Client.SetDRM(ctx, req.Data)
			return res, err

		case "meter_reverse", "ct_reverse":
			res, err := c.Client.SetMeterReverse(ctx, req.Data)
			return res, err

		case "ct":
			res, err := c.Client.SetCT(ctx, req.Data)
			return res, err

		case "alarms":
			res, err := c.Client.AlarmList(ctx)
			return res, err

		case "performance":
			res, err := c.Client.EnablePerformanceDataMode(ctx)
			return res, err

		case "bms_balance":
			res, err := c.Client.SendCommand(ctx, dtu.ActionBMSBalance, dtu.DevBMS, 1, "", []int64{inv}, nil)
			return res, err

		case "bms_reboot":
			res, err := c.Client.SendCommand(ctx, dtu.ActionBMSReboot, dtu.DevBMS, 1, "", []int64{inv}, nil)
			return res, err

		case "grid_profile_read", "grid_profile":
			res, err := c.Client.ReadGridFile(ctx)
			return res, err

		// grid_profile_write removed: profiles are read-only on this site

		case "inverter":
			if req.Reboot {
				res, err := c.Client.InverterReboot(ctx, inv)
				return res, err
			}
			res, err := c.Client.InverterPower(ctx, req.On, inv)
			return res, err

		default:
			return nil, fmt.Errorf("unknown cmd %q (bms_mode|power_limit|inverter|ping)", req.Cmd)
		}
	}
}
