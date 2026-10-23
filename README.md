# OGS-SWG

Web panel for managing **Sing-box** and **WireGuard** servers. Go backend, React frontend, SQLite storage, shipped as a single binary.

<img alt="OGS-SWG dashboard" src="docs/images/dashboard.png" width="100%" />

[Live demo](https://swg-demo.ogstra.com/) (no login required)

## Stack

Go 1.26, React 19 + TypeScript (Vite), SQLite with [sqlc](https://sqlc.dev/), [Ristretto](https://github.com/dgraph-io/ristretto) cache, [Pond](https://github.com/alitto/pond) worker pools, [Cleanenv](https://github.com/ilyakaznacheev/cleanenv) config.

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

*   **Inbounds and users**: create and edit Sing-box inbounds and their users, with traffic limits and expiry dates.
*   **WireGuard**: manage interfaces and peers, or edit the raw `.conf`.
*   **Traffic**: per-user stats for Sing-box, per-peer stats for WireGuard.
*   **Subscriptions**: tokenized bundles with per-member aliases, quotas, refresh headers, request history, QR codes (Direct, Happ, Shadowrocket) and protection rules (token/IP block, allowlist, rate limit, browser filtering). Devices are tracked by HWID and can be moved to another subscription.
*   **Services**: restart or reload Sing-box and WireGuard, tail logs, edit whitelisted sysctl values and the raw Sing-box config.
*   **Notifications**: [ntfy](https://ntfy.sh) alerts for service down, high traffic, config apply failures and new or moved devices.

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

The panel runs in a container on the same host as sing-box and wg-quick (systemd). Files are bind-mounted; service, log, sysctl and wg commands go through `systemd-run`.

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

Compose file: [`docker/docker-local/docker-compose.yml`](docker/docker-local/docker-compose.yml).

If sing-box logs elsewhere, set `OGS_ACCESS_LOG_HOST_DIR` to that directory and point `OGS_ACCESS_LOG_PATH` at the file under `/app/access-logs`.

For External Profiles with dynamic IP files, set `OGS_EXTERNAL_PROFILE_IP_HOST_DIR` to the host directory and use the container path in the UI, e.g. `/app/external-profile-ip/sing-box` for a file named `sing-box`.

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

Settings come from `config.json`, `.env` and environment variables. This is a subset; the full list and the `OGS_*` variable names are in [`internal/core/config.go`](internal/core/config.go).

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
    "in-reality"
  ],
  "stats_inbounds": [
    "in-reality"
  ],
  "stats_outbounds": [
    "direct"
  ],
  "sampler_interval_sec": 120,
  "active_threshold_bytes": 1024,
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

Add more tags to `managed_inbounds` and `stats_inbounds` to manage several inbounds.

## Deploy

Blue/green deployment through GitHub Actions, with a watchdog, health checks and automatic rollback. See [DEPLOY_GITHUB_ACTIONS.md](DEPLOY_GITHUB_ACTIONS.md). The public demo deploys separately via `deploy-demo.yml`.

## Development

```bash
go test ./...
cd frontend && npm run build
```

Both must pass before committing; see [AGENTS.md](AGENTS.md). Build and run helpers are in `scripts/`.

## Sing-box Build Requirements

The panel needs sing-box built with these tags.

| Tag | Purpose |
|-----|---------|
| `with_clash_api` | Clash API for hot config reload (zero-downtime restarts) |
| `with_v2ray_api` | V2Ray StatsService for per-user traffic stats |
| `with_gvisor` | Network stack for tun mode |
| `with_quic` | QUIC transport (required for Hysteria2) |
| `with_utls` | uTLS fingerprinting for Reality |
| `badlinkname` | Required for Go 1.24+ compatibility |

To build from source:

```bash
go build -v -tags "with_clash_api,with_v2ray_api,with_gvisor,with_quic,with_utls,badlinkname" \
    ./cmd/sing-box
```

The [official binaries](https://sing-box.sagernet.org/) already include all of them.

### Clash API

Add a `clash_api` block under `experimental` in the sing-box config:

```json
"experimental": {
  "clash_api": {
    "external_controller": "127.0.0.1:9090"
  }
}
```

With `external_controller` set, the panel reloads config through `PUT /configs` instead of restarting the service. If `secret` is set, the panel sends it as a Bearer token.

> **Docker Local:** inside the panel container `127.0.0.1` is the container itself. Bind `external_controller` to the Docker bridge IP (usually `172.17.0.1:9090`) or `0.0.0.0:9090`.

## License

MIT
