# Hoymiles local protocol (WiFi DTU / HAS hybrid)

Reverse-engineered from:
- `suaveolent/hoymiles-wifi` (protobuf library)
- `suaveolent/ha-hoymiles-wifi` (Home Assistant integration)
- Live capture against **HAS-3.0LV-EUG1** + DTU at `10.0.0.160`

> This is **not Modbus TCP**. The DTU speaks a proprietary binary protocol
> on **TCP port 10081**. Port 502 is closed.

---

## 1. Transport

| Item | Value |
|------|--------|
| Host | `10.0.0.160` (`hoymiles.lan`) |
| Port | **10081** |
| Framing | Binary, big-endian |
| Payload | Protocol Buffer |
| Optional crypto | AES-128-GCM |
| Integrity | CRC-16/MODBUS (poly `0x18005`, rev, init `0xFFFF`) |
| Rate limit | ≈1 request / 2 s (library enforces); firmware may cap ~30–120 s for cloud coexistence |
| Connections | Effectively single-client — concurrent pollers starve each other |

---

## 2. Frame layout

### 2.1 Standard (app ↔ DTU)

```
offset  size  field
0       2     magic          = "HM" (0x48 0x4D)
2       2     command tag    u16be  (request uses 0xA3xx, reply 0xA2xx / 0x83xx / 0xC3xx / 0xDBxx)
4       2     sequence       u16be  (increments per request)
6       2     crc16          u16be  over payload only
8       2     length         u16be  = 10 + payload_len   (or 10 + ciphertext_len-16 when encrypted)
10      N     payload        protobuf  (or AES-GCM ciphertext||tag16)
```

### 2.2 Extended (energy-storage / gateway)

Used for hybrid HAS/HIT/HYS. Carries DTU serial and a “number” field.

```
offset  size  field
0       2     magic "HM"
2       2     command tag
4       2     sequence
6       2     crc16 over bytes [24 .. length)
8       2     length         = 24 + payload_len
10      2     fixed          = 14
12      8     dtu_serial     u64be
20      2     reserved       = 0
22      2     number         u16be  (0 or 1 for ES; 255 for gateway)
24      N     payload        protobuf (NOT AES-GCM)
```

---

## 3. Encryption (when enabled)

Flag: `APPInfo.dtu_info.dfs` bit **25** (`IS_ENCRYPTED_BIT_INDEX`).

```
enc_rand = dtu_info.enc_rand          # 16 bytes
key      = SHA256(SHA256(SHA256(enc_rand)))[:16]
nonce    = SHA256(SHA256(SHA256(le16(tag) || le16(seq) || enc_rand)))[-12:]
aad      = le16(tag) || le16(seq)
```

AES-128-GCM over the protobuf body. Extended-format ES frames are **never**
GCM-wrapped (CRC covers the raw protobuf).

Unencrypted command tags: `APP_INFO` req/res and heartbeat.

---

## 4. Command tags (request = App → DTU)

| Tag | Hex | Protobuf request / response | Purpose |
|-----|-----|-----------------------------|---------|
| APP info | `a3 01` | `APPInfoDataResDTO` → `APPInfoDataReqDTO` | DTU identity, `dfs`, `enc_rand` |
| Heartbeat | `a3 02` | `HBResDTO` → `HBReqDTO` | keep-alive |
| Real data (old) | `a3 03` | `RealDataResDTO` → `RealDataReqDTO` | microinverter real-time |
| Command | `a3 05` | `CommandResDTO` → `CommandReqDTO` | actions (power limit, reboot, …) |
| Get config | `a3 09` | `GetConfigResDTO` → `GetConfigReqDTO` | WiFi/network config |
| Real data new | `a3 11` | `RealDataNewResDTO` → `RealDataNewReqDTO` | paged real-time (`cp`/`ap`) |
| Hist power | `a3 15` | `AppGetHistPowerResDTO` → `…ReqDTO` | historical power |
| **ES registry** | `c3 02` | `ESRegResDTO` → `ESRegReqDTO` | hybrid inverters + meter |
| **ES real-time** | `c3 03` | `ESDataResDTO` → `ESDataReqDTO` | hybrid live data (CT, BMS, flows) |
| **ES user set** | `c3 08` | `ESUserSetPutResDTO` → `…PutReqDTO` | working mode / TOU / peak shave |
| GW info | `db 01` | `GWInfoResDTO` → `GWInfoReqDTO` | gateway identity |
| GW net info | `db 06` | `GWNetInfoRes` → `GWNetInfoReq` | cellular/WiFi signal |

Hybrid discovery path (works on HAS-3.0LV-EUG1):

1. `ES_REG` with DTU serial → inverters + `model_name` + `bms_cap` + `pv_num`
2. `ES_DATA` with DTU serial + inverter serial → live telemetry
3. `GW_NET` with DTU serial → link state / RSSI (`csq`)

> On this firmware `APP_INFO`, `REAL_DATA_NEW`, and `GW_INFO` often return
> no body / parse-fail. **ES extended frames always answer** once you know
> the serials (they are in the HA config entry).

---

## 5. Device identifiers (this installation)

| Field | Value |
|-------|-------|
| DTU serial | `73672166638741` |
| Inverter serial | `35734734571570` |
| Model | **HAS-3.0LV-EUG1** (AC-coupled hybrid, 3 kW, 2×CT meter) |
| Main SW | `V01.11.02` |
| Sub SW | `V01.05.09` |
| Sys SW | `V02.07.07` |
| BMS capacity | `51` (0.1 kWh → 5.1 kWh) |
| BMS type | `1` |
| PV inputs | `2` |
| Meter | serial `1`, `sn_pv 1` |
| `dfs` | `655642` (bit 25 = 0 → unencrypted) |
| Network | `net_mod=1` (WiFi), `csq=100`, `net_state=1` |

---

## 6. ES real-time field map & scales

Scales match `ha-hoymiles-wifi` conversion factors and were verified against
live numbers (V·I ≈ √(P²+Q²)).

### Battery (`DBmsMO`)

| Field | Scale | Unit | Notes |
|-------|-------|------|-------|
| `state_of_charge` | 1 | % | 20 |
| `state_of_health` | 1 | % | 100 |
| `voltage` | 0.1 | V | 520 → 52.0 V |
| `current` | 1 | A? | often 0/1 when idle |
| `power` | 1 | W | signed |
| `internal_charge_mode` | 0.1 | A | 1000 → 100 A limit |
| `internal_discharge_mode` | 0.1 | A | 1000 → 100 A limit |
| `cell_voltage_high` | 0.01 | V | |
| `cell_voltage_low` | 0.01 | V | |
| `temp_high_charge` | 0.1 | °C | 220 → 22.0 °C |
| `temp_low_charge` | 0.1 | °C | |
| `voltage_charge_high` | 0.001 | V | cell OV threshold |
| `voltage_charge_low` | 0.001 | V | cell UV threshold |
| `temp_*_module` / `voltage_module_*` | — | — | `32767` = N/A |
| `energy_charged` / `energy_discharged` | 0.1 | kWh | lifetime |

### Grid / Load / Inverter phases

| Field | Scale | Unit |
|-------|-------|------|
| `voltage` | 0.1 | V |
| `current` | 0.01 | A |
| `active_power` | 1 | W |
| `reactive_power` | 1 | var |
| `power_factor` | 0.01 | (cos φ) |
| `frequency` | 0.01 | Hz |
| `energy_consumed` | 0.1 | kWh |
| `dc_voltage` | 0.1 | V |
| `dc_current` | 0.01 | A |
| `eps_*` | same as AC | backup port |

### Power flow (`DFlowMO`, watts)

| Field | Meaning |
|-------|---------|
| `pv_to_load` | PV → house |
| `pv_to_battery` | PV → battery |
| `pv_to_grid` | PV → grid export |
| `battery_to_load` | battery → house |
| `grid_to_load` | grid → house |
| `battery_to_grid` | battery → grid export |
| `state_of_charge` | SOC echo |

### Sign convention (observed)

With house load ≈ 112 W and `power_flow.grid_to_load` ≈ 128 W:

- `grid.phases[0].active_power` ≈ **−128 W** → **negative = importing from grid**
- `inverter.phases[0].active_power` ≈ **−16 W** → **negative = inverter absorbing** (charging)
- `load.phases[0].active_power` ≈ **+112 W** → positive = consumption

CT2 measures the load branch; CT1 the grid branch (HAS 2×CT kit).

---

## 7. Working modes (`ESUserSet`, `BMSWorkingMode`)

| Value | Name | Extra fields |
|------:|------|--------------|
| 1 | `SELF_USE` | `rev_soc`, `max_power` |
| 2 | `ECONOMIC` | tariff date/week blocks (`peak/valley/partial` in/out prices + times) |
| 3 | `BACKUP_POWER` | `rev_soc` |
| 4 | `PURE_OFF_GRID` | — |
| 5 | `FORCED_CHARGING` | `max_power` |
| 6 | `FORCED_DISCHARGE` | `max_power` |
| 7 | `PEAK_SHAVING` | `peak_soc`, `peak_meterpwr` |
| 8 | `TIME_OF_USE` | `tou[]` charge/discharge windows, power, min/max SOC |

Write path: `CMD_ES_USER_SET` (`c3 08`) → `ESUserSetPutResDTO`.
Home Assistant service: `hoymiles_wifi.set_bms_mode`.

### Command actions (`CommandPB.action`) of interest

| Action | ID | Use |
|--------|---:|-----|
| LIMIT_POWER | 8 | export power limit (⚠ EEPROM wear — avoid zero-feed-in loops) |
| REFLUX_CONTROL | 9 | export control |
| CT_SET | 11 | CT configuration |
| METER_REVERSE | 49 | reverse CT direction |
| ALARM_LIST | 50 | pull alarms |
| INV_BOOT_UP / SHUTDOWN / REBOOT | 8193–8195 | inverter lifecycle |
| BMS_BALANCE / URGENT_CHARGING | 8208 / 8225 | battery service |
| PERFORMANCE_DATA_MODE | 33 | higher-rate telemetry (must re-enable on reboot) |

---

## 8. Wire example (ES_DATA request)

```
48 4d c3 03          HM + CMD_ES_DATA
00 07                seq
xx xx                crc16
00 3c                length = 24 + payload
00 0e                fixed 14
00 06 a4 5e 5b 3c 6e de   dtu_serial 73672166638741
00 00                reserved
00 01                number = 1
<payload>            ESDataResDTO protobuf
```

---

## 9. Practical notes

1. **Do not poll faster than ~2 s.** Cloud S-Miles and local clients share one
   logical channel; aggressive local polling can drop the cloud app.
2. Prefer **ES_REG / ES_DATA** for hybrids. Standard `REAL_DATA_NEW` is for
   microinverter DTUs.
3. Serials are numeric `int64` on the wire, stringified in HA config.
4. `32767` is the N/A sentinel for unused BMS module metrics.
5. Keep the integration update interval ≥ 35 s (this site uses 35 s) so the
   official app keeps working.
