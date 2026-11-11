# Docker Build & Deployment Guide

This guide covers building, configuring, and deploying `hoymiles-gateway` using Docker and Docker Compose.

---

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Environment Variables](#environment-variables)
  - [DTU & Inverter Configuration](#dtu--inverter-configuration)
  - [Security & Encryption](#security--encryption)
  - [MQTT & Home Assistant Discovery](#mqtt--home-assistant-discovery)
  - [Telemetry History (SQLite)](#telemetry-history-sqlite)
- [Building the Docker Image](#building-the-docker-image)
  - [Local Build](#local-build)
  - [Multi-Architecture Build (AMD64 / ARM64 / Raspberry Pi)](#multi-architecture-build-amd64--arm64--raspberry-pi)
- [Deploying with Docker Compose (Recommended)](#deploying-with-docker-compose-recommended)
  - [1. Prepare Environment & Data Directory](#1-prepare-environment--data-directory)
  - [2. Review Network & Port Mappings](#2-review-network--port-mappings)
  - [3. Start the Container](#3-start-the-container)
- [Deploying with Docker CLI (`docker run`)](#deploying-with-docker-cli-docker-run)
- [Health & Verification](#health--verification)
- [Troubleshooting & Common Pitfalls](#troubleshooting--common-pitfalls)

---

## Overview

The `hoymiles-gateway` container image uses a multi-stage build:
1. **Build stage**: Alpine Linux with Go compiler producing a static, stripped binary (`CGO_ENABLED=0`).
2. **Runtime stage**: `gcr.io/distroless/static-debian12:nonroot`, providing a hardened attack surface with no shell, package manager, or root user privileges (runs as UID `65532`).

Default listening port: **`8090`**.

---

## Prerequisites

- [Docker Engine](https://docs.docker.com/engine/install/) 20.10+ and [Docker Compose](https://docs.docker.com/compose/) v2.
- The local IP address or hostname of your Hoymiles DTU.
- The serial numbers for your DTU and hybrid inverter.
- Network connectivity between the Docker host and the DTU on TCP port `10081`.

---

## Environment Variables

All settings can be configured via environment variables (in `.env` or passed to `docker run`).

### DTU & Inverter Configuration

| Variable | Required | Default | Description |
|---|:---:|:---:|---|
| `HOYMILES_HOST` | **Yes** | — | IP address or hostname of the Hoymiles DTU (e.g. `192.168.1.50`). |
| `HOYMILES_PORT` | No | `10081` | TCP port exposed by the DTU. |
| `HOYMILES_DTU_SERIAL` | **Yes** | — | 12- to 14-digit numeric serial number of the DTU (e.g. `104162345678`). |
| `HOYMILES_INV_SERIAL` | **Yes** | — | Numeric serial number of the hybrid inverter (e.g. `114162345678`). |
| `HOYMILES_LISTEN` | No | `:8090` | Container bind address and HTTP port for the REST/SSE server. |
| `HOYMILES_POLL` | No | `30s` (Compose) / `5s` (App) | Interval between DTU poll requests (e.g. `5s`, `15s`, `30s`, `1m`). Enforces minimum `2s`. |
| `HOYMILES_TIMEOUT` | No | `12s` | Per-request network timeout for DTU TCP communication. |

### Security & Encryption

| Variable | Required | Default | Description |
|---|:---:|:---:|---|
| `HOYMILES_ENCRYPTED` | No | `""` (disabled) | Set to `1` to enable AES-128-GCM communication if your DTU requires encryption (DFS bit 25 enabled). |
| `HOYMILES_ENC_RAND` | No* | `""` | 32-character hexadecimal string (16 raw bytes) representing the DTU encryption nonce. *Required only when `HOYMILES_ENCRYPTED=1`. |

### MQTT & Home Assistant Discovery

| Variable | Required | Default | Description |
|---|:---:|:---:|---|
| `HOYMILES_MQTT` | No | `""` (disabled) | MQTT broker URL (e.g. `tcp://192.168.1.10:1883`, `ssl://mqtt.example.com:8883`). If omitted, MQTT publishing and command listeners are disabled. |
| `HOYMILES_MQTT_USER` | No | `""` | MQTT username (leave empty if broker has anonymous access enabled). |
| `HOYMILES_MQTT_PASS` | No | `""` | MQTT password. |
| `HOYMILES_MQTT_TOPIC` | No | `hoymiles/has` | Base MQTT topic prefix for gateway telemetry, state, and commands. |
| `HOYMILES_MQTT_DISCOVERY` | No | `homeassistant` | MQTT auto-discovery prefix used by Home Assistant. |
| `HOYMILES_MQTT_CLIENT_ID` | No | `hoymiles-gateway` | Unique MQTT client identifier passed to the broker. |
| `HOYMILES_DEVICE_NAME` | No | `Hoymiles HAS` | Device name displayed in Home Assistant integrations and entity registry. |

### Telemetry History (SQLite)

| Variable | Required | Default | Description |
|---|:---:|:---:|---|
| `HOYMILES_DB` | No | `/data/history.db` | File path inside the container for the SQLite telemetry database. Set to empty string `""` to disable persistent local history. |
| `HOYMILES_HISTORY_DAYS` | No | `90` | Number of days to retain historical telemetry samples. Records older than this are automatically pruned daily. Set to `0` to disable pruning. |

---

## Building the Docker Image

### Local Build

Build the image locally with the tag `hoymiles-gateway:latest`:

```bash
docker build -t hoymiles-gateway:latest .
```

### Multi-Architecture Build (AMD64 / ARM64 / Raspberry Pi)

To build and push images supporting multiple architectures (e.g., standard x86 servers and ARM-based single-board computers like Raspberry Pi):

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t your-registry/hoymiles-gateway:latest \
  --push .
```

---

## Deploying with Docker Compose (Recommended)

### 1. Prepare Environment & Data Directory

Copy the sample environment file and create the data directory for SQLite persistence:

```bash
cp .env.example .env
mkdir -p data
```

> [!IMPORTANT]
> The container runs as the non-root user `nonroot` (UID `65532`, GID `65532`). Ensure the `./data` directory on the host is writable by this UID:
> ```bash
> chown -R 65532:65532 data
> # or alternatively:
> chmod 777 data
> ```

Edit `.env` and fill in your actual DTU and inverter details:

```env
HOYMILES_HOST=192.168.1.50
HOYMILES_PORT=10081
HOYMILES_LISTEN=:8090
HOYMILES_DTU_SERIAL=104162000000
HOYMILES_INV_SERIAL=114162000000
HOYMILES_POLL=30s
HOYMILES_TIMEOUT=12s

# Optional: MQTT + Home Assistant
HOYMILES_MQTT=tcp://192.168.1.10:1883
HOYMILES_MQTT_USER=
HOYMILES_MQTT_PASS=
HOYMILES_MQTT_TOPIC=hoymiles/has
HOYMILES_MQTT_DISCOVERY=homeassistant
HOYMILES_MQTT_CLIENT_ID=hoymiles-gateway
HOYMILES_DEVICE_NAME=Hoymiles HAS

# SQLite history
HOYMILES_DB=/data/history.db
HOYMILES_HISTORY_DAYS=90
```

### 2. Review Network & Port Mappings

The default `docker-compose.yml` is configured to connect to an external reverse-proxy network (`proxy-network`):

```yaml
networks:
  proxy-network:
    external: true
```

#### Option A: Running behind an existing reverse proxy (Traefik / Nginx Proxy Manager)
Ensure the external network exists before starting:
```bash
docker network create proxy-network
```

#### Option B: Standalone mode (Direct Port Publishing)
If you are **not** using a reverse proxy and want to expose port `8090` directly on the host, update `docker-compose.yml`:

```yaml
services:
  hoymiles-gateway:
    container_name: hoymiles-gateway
    build: .
    image: hoymiles-gateway:latest
    restart: unless-stopped
    ports:
      - "8090:8090"
    environment:
      HOYMILES_HOST: ${HOYMILES_HOST}
      HOYMILES_PORT: ${HOYMILES_PORT:-10081}
      HOYMILES_LISTEN: ${HOYMILES_LISTEN:-:8090}
      HOYMILES_DTU_SERIAL: ${HOYMILES_DTU_SERIAL}
      HOYMILES_INV_SERIAL: ${HOYMILES_INV_SERIAL}
      HOYMILES_POLL: ${HOYMILES_POLL:-30s}
      HOYMILES_TIMEOUT: ${HOYMILES_TIMEOUT:-12s}
      HOYMILES_ENCRYPTED: ${HOYMILES_ENCRYPTED:-}
      HOYMILES_ENC_RAND: ${HOYMILES_ENC_RAND:-}
      HOYMILES_MQTT: ${HOYMILES_MQTT:-}
      HOYMILES_MQTT_USER: ${HOYMILES_MQTT_USER:-}
      HOYMILES_MQTT_PASS: ${HOYMILES_MQTT_PASS:-}
      HOYMILES_MQTT_TOPIC: ${HOYMILES_MQTT_TOPIC:-hoymiles/has}
      HOYMILES_MQTT_DISCOVERY: ${HOYMILES_MQTT_DISCOVERY:-homeassistant}
      HOYMILES_MQTT_CLIENT_ID: ${HOYMILES_MQTT_CLIENT_ID:-hoymiles-gateway}
      HOYMILES_DEVICE_NAME: ${HOYMILES_DEVICE_NAME:-Hoymiles HAS}
      HOYMILES_DB: ${HOYMILES_DB:-/data/history.db}
      HOYMILES_HISTORY_DAYS: ${HOYMILES_HISTORY_DAYS:-90}
    volumes:
      - ./data:/data
```

### 3. Start the Container

```bash
# Build and start in background
docker compose up -d --build

# Follow logs
docker compose logs -f

# Check status
docker compose ps

# Stop the container
docker compose down
```

---

## Deploying with Docker CLI (`docker run`)

To run the container directly without Compose:

```bash
# Create and set permissions on data directory
mkdir -p $(pwd)/data
chmod 777 $(pwd)/data

docker run -d \
  --name hoymiles-gateway \
  --restart unless-stopped \
  -p 8090:8090 \
  -v $(pwd)/data:/data \
  -e HOYMILES_HOST="192.168.1.50" \
  -e HOYMILES_PORT="10081" \
  -e HOYMILES_DTU_SERIAL="104162000000" \
  -e HOYMILES_INV_SERIAL="114162000000" \
  -e HOYMILES_POLL="30s" \
  -e HOYMILES_MQTT="tcp://192.168.1.10:1883" \
  -e HOYMILES_DB="/data/history.db" \
  hoymiles-gateway:latest
```

---

## Health & Verification

Once deployed, verify that the gateway is running and communicating with the DTU:

### 1. Health Endpoint
```bash
curl -s http://localhost:8090/health | jq
```
Expected output:
```json
{
  "status": "ok",
  "dtu_connected": true,
  "last_update": "2026-11-11T23:55:00Z"
}
```

### 2. Live Telemetry
```bash
curl -s http://localhost:8090/api/v1/state | jq
```

### 3. Real-Time Server-Sent Events (SSE) Stream
```bash
curl -N http://localhost:8090/api/v1/stream
```

### 4. Home Assistant & MQTT
Check if the entities are populated in Home Assistant under **Settings > Devices & Services > MQTT**. You should see **Hoymiles HAS** with 27 auto-discovered sensors.

---

## Troubleshooting & Common Pitfalls

### 1. `permission denied` opening `/data/history.db`
- **Cause**: The container runs under non-root UID `65532`, but the host `./data` directory was created by `root` or another user without write permissions.
- **Fix**: Run `chmod 777 ./data` or `chown -R 65532:65532 ./data` on the host.

### 2. `network proxy-network declared as external, but could not be found`
- **Cause**: Compose expects an existing Docker network named `proxy-network`.
- **Fix**: Create it using `docker network create proxy-network`, or switch `docker-compose.yml` to standard port mapping (`ports: - "8090:8090"`) as shown in [Option B](#option-b-standalone-mode-direct-port-publishing).

### 3. DTU Timeouts or Connection Refused
- **Cause**: Hoymiles DTUs accept only **one active TCP connection at a time**. If official cloud app or another script is holding port `10081`, connection attempts will fail.
- **Fix**: Ensure no other local polling scripts or connections are active. Hoymiles cloud monitoring will still function when the DTU is idle between polls. Keep `HOYMILES_POLL` at `30s` (or at least `≥ 5s`) to prevent serial bus starvation.

### 4. Viewing Container Logs
Check logs for protocol errors or handshake failures:
```bash
docker compose logs -f --tail=100 hoymiles-gateway
```

