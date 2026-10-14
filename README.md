# hoymiles-gateway

Go gateway for Hoymiles **HAS hybrid** inverters (WiFi DTU, TCP **10081**).

Owns the single DTU session and fans out to many HTTP clients:

| Endpoint | Description |
|----------|-------------|
| `GET /health` | liveness + last update |
| `GET /api/v1/registry` | hybrid inverters, firmware, meter |
| `GET /api/v1/state` | latest scaled telemetry (CT, BMS, power flow) |
| `GET /api/v1/network` | WiFi/link status |
| `GET /api/v1/stream` | Server-Sent Events live snapshots |
| `POST /api/v1/bms/mode` | set working mode (app parity) |

## Why

The DTU is effectively **single-client**. This daemon serializes access
(≥2 s between requests) and serves many consumers from cache — HA, Grafana,
custom dashboards — without starving each other.

## Quick start

```bash
go build -o bin/hoymiles-gateway ./cmd/gateway

./bin/hoymiles-gateway \
  -host YOUR_DTU_HOST \
  -listen :8090 \
  -dtu-serial YOUR_DTU_SERIAL \
  -inverter-serial YOUR_INV_SERIAL \
  -poll 5s
```

```bash
curl -s localhost:8090/api/v1/state | jq
curl -N localhost:8090/api/v1/stream
curl -s localhost:8090/api/v1/bms/mode \
  -H 'Content-Type: application/json' \
  -d '{"mode":"self_use","rev_soc":20,"max_power":80}'
```

## Flags / env

| Flag | Env | Default |
|------|-----|---------|
| `-host` | `HOYMILES_HOST` | `YOUR_DTU_HOST` |
| `-listen` | `HOYMILES_LISTEN` | `:8090` |
| `-dtu-serial` | `HOYMILES_DTU_SERIAL` | site DTU SN |
| `-inverter-serial` | `HOYMILES_INV_SERIAL` | site INV SN |
| `-poll` | `HOYMILES_POLL` | `5s` (min 2s) |
| `-encrypted` | `HOYMILES_ENCRYPTED=1` | off |
| `-enc-rand` | `HOYMILES_ENC_RAND` | — |

## BMS modes

`self_use` · `economic` · `backup_power` · `pure_off_grid` ·
`forced_charging` · `forced_discharge` · `peak_shaving` · `time_of_use`

```json
POST /api/v1/bms/mode
{"mode":"peak_shaving","rev_soc":20,"peak_soc":80,"peak_meter_power":3000}
```

## Protocol notes

- Not Modbus. TCP/10081, `HM` magic, CRC-16/MODBUS, protobuf.
- Hybrid uses **extended** frames (`c302` ES_REG, `c303` ES_DATA, `c308` ES_USER_SET).
- Optional AES-128-GCM when DTU `dfs` bit 25 is set.
- Scales match `ha-hoymiles-wifi` (V×0.1, A×0.01, freq×0.01, …).
- Full map: [`../docs/PROTOCOL.md`](../docs/PROTOCOL.md).

## Docker

```bash
docker build -t hoymiles-gateway .
docker run --rm -p 8090:8090 hoymiles-gateway \
  -host YOUR_DTU_HOST -dtu-serial YOUR_DTU_SERIAL -inverter-serial YOUR_INV_SERIAL
```

## systemd

See `deploy/hoymiles-gateway.service`.

## Control / grid profile API

| Endpoint | Method | Notes |
|----------|--------|-------|
| `/api/v1/command` | POST | Generic CommandPB (`action`, `data`, `mi_to_sn`) |
| `/api/v1/command/power-limit` | POST | `{"percent":80}` — export limit (EEPROM wear warning) |
| `/api/v1/grid-profile` | GET | Best-effort read (cmd 41 + APP info) |
| `/api/v1/grid-profile` | POST | Write profile — **requires** `"confirm":"I_UNDERSTAND"` |
| `/api/v1/inverter` | POST | `{"on":true}` / `{"reboot":true}` |
| `/api/v1/misc/alarms` | GET/POST | Alarm list |
| `/api/v1/misc/performance` | GET/POST | Performance data mode |
| `/api/v1/misc/ct` | POST | CT config (confirm) |
| `/api/v1/misc/drm` | POST | DRM setting |
| `/api/v1/misc/meter-reverse` | POST | CT direction (confirm) |
| `/api/v1/misc/reflux` | POST | Export/reflux control |

### Action codes (CommandPB)

`14` SET_GRID_FILE · `41` GRID_FILE_READ · `8` LIMIT_POWER · `9` REFLUX ·
`11` CT_SET · `43` DRM · `49` METER_REVERSE · `50` ALARM ·
`8193–8195` INV boot/shutdown/reboot · `33` PERFORMANCE_DATA_MODE

### Safety

Grid-code / meter / protection writes can **violate local regulations** or
damage equipment. Always export the current profile first. Dangerous actions
require `"confirm": "I_UNDERSTAND"`.

> On HAS-3.0LV hybrid firmware, CommandPB (`0xA305`) often returns empty or
> times out — ES_BMS writes (`c308`) are reliable. Grid file payload format
> is still being mapped; the API is ready for it.

## MQTT + Home Assistant discovery

```bash
./hoymiles-gateway ... \
  -mqtt tcp://YOUR_MQTT_HOST:1883 \
  -mqtt-topic hoymiles/has \
  -mqtt-discovery homeassistant
```

| Topic | Payload |
|-------|---------|
| `homeassistant/sensor/hoymiles_has_*/config` | retained HA discovery |
| `homeassistant/binary_sensor/hoymiles_has_gateway_online/config` | connectivity |
| `hoymiles/has/state` | retained JSON snapshot (all sensors) |
| `hoymiles/has/registry` | device registry |
| `hoymiles/has/availability` | `online` / `offline` |

**27 sensors** auto-create in HA under device **Hoymiles HAS**
(SOC, SOH, battery V/W/temp, grid P/V/A/Hz/energy, load, inverter, PV strings,
power-flow paths, grid import/export).

### systemd (optional)

`~/.config/systemd/user/hoymiles-gateway.service`
```bash
systemctl --user restart hoymiles-gateway
journalctl --user -u hoymiles-gateway -f
```

## Docker Compose

Installed at `/mnt/data/hoymiles-gateway/` on the Docker host.
Configuration is **env-only** (no long `command:`). Defaults match this site;
override via `.env` (see `deploy/.env.example`).

```yaml
services:
  hoymiles-gateway:
    container_name: hoymiles-gateway
    build: .
    image: hoymiles-gateway:latest
    restart: unless-stopped
    environment:
      HOYMILES_HOST: ${HOYMILES_HOST:-YOUR_DTU_HOST}
      HOYMILES_DTU_SERIAL: ${HOYMILES_DTU_SERIAL:-YOUR_DTU_SERIAL}
      HOYMILES_INV_SERIAL: ${HOYMILES_INV_SERIAL:-YOUR_INV_SERIAL}
      HOYMILES_POLL: ${HOYMILES_POLL:-30s}
      HOYMILES_MQTT: ${HOYMILES_MQTT:-tcp://YOUR_MQTT_HOST:1883}
      HOYMILES_MQTT_TOPIC: ${HOYMILES_MQTT_TOPIC:-hoymiles/has}
    networks: [proxy-network]
networks:
  proxy-network: { external: true }
```

```bash
cd /mnt/data/hoymiles-gateway
docker compose up -d
docker compose logs -f
```

### Env vars

| Variable | Default |
|----------|---------|
| `HOYMILES_HOST` | `YOUR_DTU_HOST` |
| `HOYMILES_PORT` | `10081` |
| `HOYMILES_LISTEN` | `:8090` |
| `HOYMILES_DTU_SERIAL` | site DTU SN |
| `HOYMILES_INV_SERIAL` | site INV SN |
| `HOYMILES_POLL` | `30s` |
| `HOYMILES_TIMEOUT` | `12s` |
| `HOYMILES_MQTT` | `tcp://YOUR_MQTT_HOST:1883` |
| `HOYMILES_MQTT_TOPIC` | `hoymiles/has` |
| `HOYMILES_MQTT_DISCOVERY` | `homeassistant` |
| `HOYMILES_DEVICE_NAME` | `Hoymiles HAS` |
| `HOYMILES_ENCRYPTED` / `HOYMILES_ENC_RAND` | empty (unencrypted) |

### Caddy

```caddy
@hoymiles host hoymiles.example.com
handle @hoymiles {
    reverse_proxy hoymiles-gateway:8090
}
```

URL: **https://hoymiles.example.com/** (LAN / VPN via `internal_only`)

> systemd user unit was removed — Docker Compose only.

## MQTT commands

Subscribe / publish:

| Topic | Direction | Payload |
|-------|-----------|---------|
| `hoymiles/has/cmd` | → gateway | JSON command |
| `hoymiles/has/cmd/result` | ← gateway | `{"ok":true,"result":...}` |
| `hoymiles/has/last_update` | ← gateway | ISO-8601 timestamp |

```bash
mosquitto_pub -h YOUR_MQTT_HOST -t hoymiles/has/cmd -m '{"cmd":"ping"}'
mosquitto_pub -h YOUR_MQTT_HOST -t hoymiles/has/cmd -m '{"cmd":"bms_mode","mode":"self_use","rev_soc":20,"max_power":80}'
mosquitto_pub -h YOUR_MQTT_HOST -t hoymiles/has/cmd -m '{"cmd":"power_limit","percent":80}'
mosquitto_pub -h YOUR_MQTT_HOST -t hoymiles/has/cmd -m '{"cmd":"inverter","on":true}'
mosquitto_pub -h YOUR_MQTT_HOST -t hoymiles/has/cmd -m '{"cmd":"inverter","reboot":true}'
```

## Availability (`expire_after`)

All discovery entities include `expire_after` (default **120s** ≈ 3× poll + 30s).
If the gateway/DTU stops publishing, HA marks sensors **unavailable** instead of
stuck values. Diagnostic `sensor.hoymiles_has_last_update` shows the last success.
