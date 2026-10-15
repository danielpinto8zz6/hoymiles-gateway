# Grid profile / network file — RE notes (S-Miles Installer 3.13.1)

## Where it lives in the app

| Piece | Location |
|-------|----------|
| UI | `LdEsMainCbActivity` → `tv_network_file_settings` → `LdEsAdvanceCfgActivity` |
| Check UI | `GridFileCheckActivity` / `GridFileCheckVM` |
| Cloud select | `GET/POST /pvm-gf/api/0/grid_profile/scc_g_a` (`PVM_GRID_PROFILE_SELECT_BY_COUNTRY_CODE`) |
| Cloud put | `/pvm-ctl/api/0/dev/config/put_grid_profile` (`PVM-CTL_CONFIG_PUT_GRID_PROFILE`) |
| Local command | `ECommandAction.SET_GRID_FILE` = **14** (matches `CMD_ACTION_SET_GRID_FILE`) |
| Read command | **41** `GRID_FILE_READ` |

## Protobuf (`CfgRuleConfigPBALL.GridFile*`)

```proto
message GridFileDataDTO {
  GridFileRuleBO rule = 1;
  GridFileConfigBO cfg = 2;
}
message GridFileRuleBO {
  int32 rid = 1;
  int32 grid_id = 2;
  int32 min_ver = 3;
  int32 max_ver = 4;
  string prefix = 5;
}
message GridFileConfigBO {
  int32 id = 1;
  int32 ver = 2;
  string name = 3;        // profile name, e.g. grid standard
  int32 country_std = 4;
  string country_code = 5;
  repeated CfgItemBO list = 6;  // parameter rows
}
message CfgItemBO {
  int32 id = 1; int32 stc_id = 2;
  string name = 3; string key = 4;
  int32 type = 6; string mark = 7;
  map<string,string> content = 8;
  int32 position = 9; string unit = 10;
  double min = 11; double max = 12;
  double multiple = 13; double scale = 14;
  int32 length = 15; int32 show = 16; int32 edit = 17; int32 change = 18;
  int32 card_idx = 20; string remarks = 22;
}
```

Go bindings: `hoymiles-gateway/pb/GridFile.pb.go`.

## Field catalog (ESS advanced / network file, `es_rules_default_bw.json`)

Editable keys (with unit / range):

| key | meaning | unit | range |
|-----|---------|------|-------|
| `meter_model` | meter model | | |
| `grid_meter_reverse` | grid CT reverse | | |
| `grid_meter_ir` | grid CT ratio | | 0–9999 |
| `pv_meter_reverse` | PV CT reverse | | |
| `pv_meter_ir` | PV CT ratio | | 0–9999 |
| `display_brightness` | panel brightness | | 1–10 |
| `gen_port_mode` | generator port mode | | |
| `bms485_com_type` | BMS RS485 type | | |
| `grid_import_power_limit` | max grid import | W | 0–20000 |
| `three_phase_unbalance_en` | 3-phase unbalance | | |
| `bat_max_dischrg_pwr` | max discharge | % | 50–100 |
| `bat_max_chrg_pwr` | max charge | % | 50–100 |
| `max_soc` / `min_soc` | battery SOC limits | % | 70–100 / 10–50 |
| `bat_supplementary_power` | battery supplement | W | 200–1000 |
| `rsvd_soc_supply_power` | reserved SOC supply | % | 0–100 |
| `bat_feed_power_in_peak` | peak feed-in | % | 0–100 |
| `bat_dischrg_pwr_partial` | partial-peak discharge | % | 0–100 |
| `mppt_global_scan_en` | MPPT scan | | |
| `eps_mode` | EPS mode | | |
| `ext_bypss_switch` | external bypass | | |
| `pv_offgrid_mode_en` | PV off-grid enable | | |
| `gen_*` | generator limits/timers | W/V/Hz/min | see JSON |

## App mutual exclusions

Export management **XOR** DRM **XOR** power balance **XOR** time-based output limit.

## Local gateway

- `GET /api/v1/grid-profile` — best-effort read (Command 41 + APP info)
- `POST /api/v1/grid-profile` — write (requires `"confirm":"I_UNDERSTAND"`)
- MQTT: `{"cmd":"grid_profile_read"}` / `{"cmd":"grid_profile_write","data":"..."}`

**Status:** schema fully mapped. Live CommandPB on HAS-3.0LV often returns empty;
profile *catalog* is served by Hoymiles cloud (`euapi.hoymiles.com`) and applied
via action 14. A full offline editor needs either a successful local read blob
or a cloud profile download + action-14 write path.

## Safety

Grid profiles **must** match local grid codes. Incorrect values can violate
regulations, trip protection, or damage equipment. Always export/backup first.


## Captured profile (this site)

From S-Miles Cloud web (DevTools):

| Field | Value |
|-------|-------|
| Name | **PT_EN50549-1** |
| Version | **2.2.0** |
| Country | PT |
| Cloud id | 45533 |
| Items | 71 (headers + params) |

Saved: `grid-profile/PT_EN50549-1_v2.2.0.json`  
Markdown: `grid-profile/PT_EN50549-1.md`  
HA view: **Dashboard-casa → Grid Profile**

### Key live values (PT EN50549-1)

| Section | Param | Value |
|---------|-------|------:|
| H/LV Trip | LV1 / MTT | 195.5 V / 1.5 s |
| | HV1 / MTT | 264.5 V / 0.2 s |
| | LV2 / MTT | 161 V / 0.2 s |
| | HV2 / MTT | 276 V / 0.1 s |
| | AHV 10min | 253 V |
| H/LFRT | LF1 / MTT | 47.5 Hz / 2 s |
| | HF1 / MTT | 51.5 Hz / 2 s |
| | LF2 / MTT | 47 Hz / 0.5 s |
| | HF2 / MTT | 52 Hz / 0.5 s |
| Islanding | enabled | 1 |
| Reconnect | CT / RT | 60 s / 60 s |
| | RHV / RLV | 253 V / 195.5 V |
| Ramp | RUR_NM / RUR_SS | 20 / 0.16 Rated%/s |
| F/W | Fstart / slope | 50.2 Hz / 40 Pn%/Hz |
| SPF | PF | 1.0 (off) |
| APC | prio APC/FW/UF/VW | 2/1/2/2 |
| RPC | VAR | 0 %Sn (off) |

## Policy (this site)

**Grid profile is READ-ONLY.** The HA view is documentation only.
Do not implement a write UI or routine apply. Command 14 remains available
in the gateway only for emergency/manual use with explicit confirm.

## Energy dashboard note (2026-10-13)

`sensor.ddsu666_modbus_emulator_grid_import_energy` / `..._export_energy`
**flip between two series** (e.g. 45.7 ↔ 272.9 kWh). As `total_increasing`
this fabricates ~227 kWh steps and corrupts Energy totals (5,216 kWh).

Energy dashboard uses **`sensor.entrance_main_em_energy`** /
`..._returned` (stable, monotonic) until the DDSU emulator is fixed.
