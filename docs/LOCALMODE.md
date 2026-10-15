# S-Miles Local Mode / Toolkit — full menu map

From **S-Miles Installer V3.13.1** decompile (`LdEsMainCb` = Storage Toolkit when
phone is on **DTU/DTS Wi‑Fi**). Same plant stack as HAS hybrid.

## How local mode works

| | |
|---|---|
| Phone Wi‑Fi | Join **DTU AP / DTU Wi‑Fi** (not home internet) |
| UI entry | **View Locally** / **Stay at Toolkit** |
| Transport | Same local protocol as gateway (TCP 10081 `HM`+protobuf) |
| Cloud | Optional — Toolkit works offline once linked |

> *"Please make sure that the currently connected Wi-Fi is DTU Wi-Fi, otherwise debugging will not be possible."*

---

## Storage Toolkit (ESS) — main screen (`LdEsMainCb`)

| Menu | Goes to | Purpose |
|------|---------|---------|
| **Cloud Communication** | network settings | Server / DNS / network mode |
| **Inverter Manage** | `LdEsInvList` | Inverter list, real-time data, BMS cfg, self-test report, search again |
| **System Settings** | `LdEsSystemSet` | Plant/system parameters |
| **On-grid Self-check** | `LdEsSelfTest` | Commissioning self-test |
| **Grid Profile Config** | `LdEsAdvanceCfg` | Network/grid file (see Grid Profile view) |
| **Meter Location** | `LdEsMeterLocationSet` | CT / meter placement |
| **Gen Mode** | `LdEsGenModeSet` | Generator port mode |
| **Network Settings** | `NetworkConfigDetailGw` | Wi‑Fi / Ethernet |
| **ZW** | `LdEsZw` | Aux / ZW functions |
| **BMS Control** | `EsBmsSmartControl` | Battery smart control |
| **Firmware Update** | `LdEsDeviceUpdate` | Device upgrade |
| **Quick setup** | `LdEsQuickStep1` | CT/meter wizard, safety config |
| **DTU options** | `LdEsDeviceOptionsDtu` | BT off, factory reset |
| **EV / Pile** | `LdEsPile` | EV charger smart control |
| **DTU AP settings** | `LdDtuApSetGwPb` | Hotspot password |

---

## Inverter Manage (`LdEsInvList`)

| Item | Action |
|------|--------|
| Inverter list (SN) | `LdEsRealData` live data |
| BMS config | `LdEsBmsCfg` |
| Self-test report | `LdEsSelfTestReport` |
| Search again | `LdEsSearchAgainDebug` |
| Meter location | `LdEsMeterLocationSet` |

---

## System / Quick setup (`LdEsQuickStep1` → Step2)

| Item | Notes |
|------|-------|
| Grid / Solar / Meter / CT | Side selection |
| Safety config | `LdEsQuickStep2SettingChild1Safe` |
| Pile DC | `LdEsPileDc` |
| Inverter list v2 | `LdEsInvListV2` |
| "No meter connected" warning | Confirm CT wiring |

---

## Device options (`LdEsDeviceOptionsDtu`)

| Option | Effect |
|--------|--------|
| Disable Bluetooth | Device not discoverable by app |
| Restore default / factory reset | Wipes device settings |
| Command success / retry | Feedback toasts |

---

## BMS / energy (app ↔ our gateway)

| App | Gateway / MQTT |
|-----|----------------|
| ESS operating modes | `bms_mode` (8 modes) |
| Charge/discharge limit | `max_power` |
| Protection SOC | `rev_soc` |
| Battery equalization | `bms_balance` |
| Gen start/stop | gen mode (ESS rules) |
| Peak shaving / TOU | `peak_shaving` / `time_of_use` / `economic` |
| EPS smart control | `eps_mode` (ESS advanced) |
| Export management | `power_limit` / `export_limit` |
| DRM | `drm` |
| CT reverse / phase | `meter_reverse` / `ct` |
| Grid profile | **read-only** in HA (see Grid Profile) |

---

## DTU / plant local tools

| Item | Purpose |
|------|---------|
| Plant Information | Name, type, timezone, cover |
| Devices | SN list, search, QR photo add |
| Cloud Communication | Network mode (Wi‑Fi/Ethernet/SIM) |
| Auto networking | Sync topology to gateway |
| Communication quality / noise test | 1–5 min RF plan |
| Firmware upgrade | Local package / hotspot |
| Direct connection (Wi‑Fi MI) | View device without cloud |
| Local scan | P1 / DTS discovery (`P1LocalVM`) |
| Anti-theft | 8-digit password; `00000000` cancels |
| Christmas mode | Decorative |
| Language / region | App UI |

---

## Local vs cloud

| Mode | Use |
|------|-----|
| **View Locally** (Toolkit) | Installer debug, no cloud needed |
| **S-Miles Cloud** | History, remote, tariffs, VPP |
| **Gateway / HA** | Local monitoring + modes we expose |

## Dashboard

See HA **Dashboard-casa → Local Mode** for this map (read-only reference).
