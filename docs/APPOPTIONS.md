# S-Miles Installer V3.13.1 — option map

Extracted from `com.hm.hemaiInstall1` (APKPure XAPK) strings + assets
(`es_rules_*.json`). Package: `com.hm.hemaiInstall`.

## ESS operating modes (app → local)

| App label | Local / API |
|-----------|-------------|
| Spontaneous for private use | `bms_mode: self_use` (PV→load→battery→grid) |
| Economic / Time of Use | `bms_mode: economic` or `time_of_use` |
| Backup power | `bms_mode: backup_power` |
| Pure off-grid | `bms_mode: pure_off_grid` |
| Forced charging | `bms_mode: forced_charging` |
| Forced discharging | `bms_mode: forced_discharge` |
| Peak shaving | `bms_mode: peak_shaving` |
| Inspection / On-grid | commissioning (not exposed) |

## ESS / battery settings

| App label | Field |
|-----------|-------|
| Protection SoC | `rev_soc` |
| Charge power limit | `max_power` (charge) |
| Discharge power limit | `max_power` (discharge) |
| Max. BAT feed-in power in peak time | peak/TOU power |
| Charge / Discharge Power Limit | `max_power` |
| Daily charge / discharge / feed-in energy | read-only telemetry |
| Battery capacity, SOH, SN | telemetry |
| Charge/Discharge cutoff voltage | expert (rules JSON) |
| Battery Equalization | `cmd: bms_balance` (action 8208) |
| GEN start/stop SOC | generator mode |

## Tariff / TOU

| App label | Local |
|-----------|-------|
| On-Peak / Mid-Peak / Off-Peak | economic `time_settings` |
| Tariff rates (≤4) | economic prices |
| My tariff is flat / varies / market | display only |

## Grid / export (Toolkit)

| App label | Local CommandPB action |
|-----------|------------------------|
| Export Management / Export Power Limitation | `8` LIMIT_POWER, `9` REFLUX |
| Export Power Limit (phase A/B/C) | `8` with A/B/C |
| Zero Feed-in (Meter) | export limit + meter |
| Power Limit (output) | `8` |
| DRM Setting | `43` |
| Power Balance Config / Three-phase balance | `37` |
| CT Direction / CT Phase / Reverse CT | `11`, `49` METER_REVERSE |
| Meter Location | CT config |
| Grid Profile Config / Check / Update | `14` SET, `41` READ |
| Anti-theft | `5`, `22` |
| Reactive Power Control (RPC) | `47`/`48` |
| Reactive Power Compensation (Q/S) | `47`/`48` |
| Phase Power Limit Setting | `8` |
| Time-based output power control | schedule (app) |

## DTU / install

| App label | Notes |
|-----------|-------|
| Wi-Fi / Ethernet setup | SetConfig `a310` |
| DTU SN, firmware upgrade | `2` / cloud |
| Auto networking | `4101` |
| Performance data mode | `33` |
| Alarms | `50` |
| Commissioning wizard | installer only |

## Constraints (from app)

- Export management **XOR** DRM **XOR** Power balance **XOR** time-based output limit
- Grid dispatch can lock export/DRM/3-phase settings
- Anti-theft password 8 digits; `00000000` cancels
- ESS mode changes may need platform verification code in the official app

## What the gateway exposes today

MQTT `hoymiles/has/cmd` and HTTP `/api/v1/*`:
`bms_mode` (all 8 modes), `power_limit`, `inverter`, `ping`,  
plus HTTP: grid-profile read/write (confirm), CT/DRM/meter-reverse/alarms.

Not implemented (needs firmware payload RE or cloud):  
full grid-profile editor, TOU UI with 4 tariffs, anti-theft, networking wizard,
phase-sequence editor.
