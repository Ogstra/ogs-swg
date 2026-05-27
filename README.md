# OGS-SWG Panel

Unified control plane for **Sing-box** and **WireGuard** built with **Go 1.24** and **React** (Vite/TS). Distributed as a single binary with two execution modes: bare metal and Docker on the same host. Designed for zero-downtime deployments via **Blue-Green pipelines** with health-checked watchdogs and atomic rollbacks.

<img max-width="3024" max-height="1714" alt="image" src="https://github.com/user-attachments/assets/2f80f4c3-20fe-49a3-901c-f84586301cde" />

[![Live Demo](https://img.shields.io/badge/demo-live-brightgreen)](https://swg-demo.ogstra.com/)

## Live Demo

A public instance is available at **[swg-demo.ogstra.com](https://swg-demo.ogstra.com/)** — no login required.

## Tech Stack
*   **Runtime**: Go 1.24
*   **Frontend**: React 19 + TypeScript (Vite)
*   **DB**: SQLite + [sqlc](https://sqlc.dev/) (Type-safe SQL)
*   **Cache**: [Ristretto](https://github.com/dgraph-io/ristretto)
*   **Concurrency**: [Pond](https://github.com/alitto/pond) (Goroutine worker pools)
*   **Config**: [Cleanenv](https://github.com/ilyakaznacheev/cleanenv) (`.env` + JSON)
*   **Validation**: [Validator v10](https://github.com/go-playground/validator)

## Features

|             | Reality | TLS | None | TCP | WebSocket | HTTP | gRPC | HTTP Upgrade | UDP |
|-------------|:-------:|:---:|:----:|:---:|:---------:|:----:|:----:|:------------:|:---:|
| VLESS       | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| VMess       | — | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| Trojan      | — | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| Hysteria2   | — | ✓ | — | — | — | — | — | — | ✓ |
| Shadowsocks | — | — | ✓ | ✓ | — | — | — | — | ✓ |
| AnyTLS      | — | ✓ | — | ✓ | — | — | — | — | — |
| Naive       | — | ✓ | — | ✓ | — | — | — | — | ✓ |
| WireGuard   | — | — | — | — | — | — | — | — | ✓ |

*   **Inbound Management**: create and edit managed Sing-box inbounds from the UI, including Reality, WebSocket, and protocol-specific field validation.
*   **User Management**: create, edit, and delete Sing-box users per inbound; supports multiple inbound assignments, traffic limits, expiry dates, and password/key-based users for Hysteria2, Shadowsocks, AnyTLS, and Naive.
*   **Traffic Monitoring**: per-user download/upload stats for Sing-box inbounds; per-peer rx/tx stats across all WireGuard interfaces.
*   **WireGuard Interfaces**: create, configure, enable/disable, and delete WireGuard interfaces; raw `.conf` editing per interface.
*   **Subscriptions**: create tokenized subscription bundles with per-member aliases, quota limits, refresh policies, request history, and supported QR variants for Direct and Shadowrocket.
*   **Service Control**: restart/stop Sing-box and WireGuard services and reload configurations without leaving the UI.
*   **Logs**: tail and filter Sing-box access logs in real time.
*   **Sysctl**: view and update whitelisted kernel parameters (e.g. `net.ipv4.ip_forward`) directly from the panel.
*   **Raw Configuration**: full JSON editor for the Sing-box config file (Experimental).

### Subscriptions

*   Per-member aliases control the display name emitted in generated subscription links.
*   Subscription quotas can be set independently of individual user quotas.
*   Refresh behavior supports `profile-update-interval` and `update-always` headers.
*   Panel-scoped defaults can prefill refresh policy and destination settings for new subscriptions.
*   Request history records subscription activity, cache hits, detected client/device metadata, and blocked requests.
*   Protection rules support token/IP blocking, allowlisting, basic rate limiting, and optional browser/social-fetcher filtering.

## Execution Modes

The mode is selected at startup:

| Priority | Condition | Mode |
|----------|-----------|------|
| 1 | `execution_mode = docker_local` | **Docker Local** — panel in Docker on the same host as singbox/wg |
| 2 | *(default)* | **Local** — bare metal, commands run directly on the host |

### Local (bare metal)

No extra config. Run the binary directly on the host:

```bash
./ogs-swg -config config.json
```

### Docker Local

Panel runs as a Docker container on the same host as singbox and wg-quick (systemd). File operations use bind mounts; service/log/sysctl/wg commands run through `systemd-run` using host runtime sockets.

Required mounts:

```yaml
environment:
  - OGS_EXECUTION_MODE=docker_local
  - OGS_ACCESS_LOG_PATH=/app/access-logs/access.log
volumes:
  - ${OGS_ACCESS_LOG_HOST_DIR:-./data}:/app/access-logs:ro
  - ${OGS_EXTERNAL_PROFILE_IP_HOST_DIR:-./homelab/ip}:/app/external-profile-ip:ro
  - /etc/sing-box:/etc/sing-box
  - /etc/wireguard:/etc/wireguard
  - /run/dbus:/run/dbus
  - /run/systemd:/run/systemd
  - /var/log/journal:/var/log/journal:ro
  - /run/log/journal:/run/log/journal:ro
```

Ready-to-use compose at [`docker/docker-local/docker-compose.yml`](docker/docker-local/docker-compose.yml).

If sing-box writes to another host log path, mount its containing directory with `OGS_ACCESS_LOG_HOST_DIR` and point `OGS_ACCESS_LOG_PATH` at the file inside `/app/access-logs`.

For External Profiles with dynamic IP files, set `OGS_EXTERNAL_PROFILE_IP_HOST_DIR` to the host directory that contains those files and use the fixed container path in the UI. For a host file named `sing-box` inside that directory, use `/app/external-profile-ip/sing-box` in the IPv6 File Path field.

For blue/green local slots, use:

```yaml
network_mode: host
environment:
  - OGS_DOCKER_LOCAL_HOST_NETWORK=true
  - OGS_LISTEN_ADDR=:18080   # blue (green uses :18081)
```

## Quick Start

**Bare metal:**
```bash
git clone https://github.com/Ogstra/ogs-swg && cd ogs-swg
./ogs-swg -config config.json
```

**Docker Local** (panel in Docker, singbox/wg as systemd on the same host):
```bash
cd docker/docker-local
OGS_API_KEY=changeme docker compose up -d
```

**Standalone Docker compose**:
```bash
git clone https://github.com/Ogstra/ogs-swg && cd ogs-swg
docker compose up -d
```

### Bootstrap Admin

On the first run, if no admin user exists, set:
```yaml
environment:
  - OGS_ADMIN_USER=admin
  - OGS_ADMIN_PASSWORD=admin
```

## Configuration

Parameters are merged from `config.json`, `.env`, and environment variables.

```json
{
  "listen_addr": ":8080",
  "api_key": "",
  "jwt_secret": "",
  "public_ip": "",
  "enable_singbox": true,
  "enable_wireguard": true,
  "use_stats_sampler": true,
  "access_log_path": "data/access.log",
  "database_path": "./data/stats.db",
  "singbox_config_path": "/etc/sing-box/config.json",
  "singbox_api_addr": "127.0.0.1:8080",
  "wireguard_config_path": "/etc/wireguard/wg0.conf",
  "execution_mode": "",
  "managed_inbounds": [
    "in-reality",
    "in-reality-2"
  ],
  "stats_inbounds": [
    "in-reality",
    "in-reality-2"
  ],
  "stats_outbounds": [
    "direct"
  ],
  "sampler_interval_sec": 30,
  "active_threshold_bytes": 2048,
  "wg_sampler_interval_sec": 60,
  "retention_enabled": false,
  "retention_days": 90,
  "wg_retention_days": 30,
  "aggregation_enabled": false,
  "aggregation_days": 7,
  "sysctl_whitelist": [
    "net.ipv4.ip_forward",
    "net.ipv6.conf.all.forwarding"
  ]
}
```

## CI/CD

Production-ready **Blue/Green deployment** via GitHub Actions: dual-slot topology (blue/green) managed by a watchdog, atomic Nginx reload on promotion, configurable baking window, and automatic rollback on health degradation.

**[Full setup guide → DEPLOY_GITHUB_ACTIONS.md](DEPLOY_GITHUB_ACTIONS.md)**

## Sing-box Build Requirements

OGS-SWG requires sing-box to be built with specific tags for full functionality.

### Required Build Tags

| Tag | Purpose |
|-----|---------|
| `with_clash_api` | Clash API for hot config reload (zero-downtime restarts) |
| `with_v2ray_api` | V2Ray StatsService for per-user traffic stats |
| `with_gvisor` | Network stack for tun mode |
| `with_quic` | QUIC transport (required for Hysteria2) |
| `with_utls` | uTLS fingerprinting for Reality |
| `badlinkname` | Required for Go 1.24+ compatibility |

### Building from Source

```bash
go build -v -tags "with_clash_api,with_v2ray_api,with_gvisor,with_quic,with_utls,badlinkname" \
    ./cmd/sing-box
```

### Official Binaries

The official sing-box pre-built binaries from [sing-box.sagernet.org](https://sing-box.sagernet.org/) include all required tags. Operators using official binaries do not need to build from source.

### Clash API Configuration

Add a `clash_api` block to your sing-box `config.json` under `experimental`:

```json
"experimental": {
  "clash_api": {
    "external_controller": "127.0.0.1:9090"
  }
}
```

When the panel detects `clash_api.external_controller` in the sing-box config, it uses the Clash API `PUT /configs` endpoint for zero-downtime reloads instead of restarting the service.

If `secret` is set in the Clash API config, the panel includes the `Authorization: Bearer <secret>` header automatically.

> **Docker Local mode:** When sing-box and the panel run in separate containers, `127.0.0.1` inside the panel container refers to the container's own loopback, not the host. Bind `external_controller` to the Docker bridge IP (typically `172.17.0.1:9090`) or `0.0.0.0:9090` so the panel can reach the Clash API.

## License

MIT
