# OGS-SWG Panel

Unified control plane for **Sing-box** (**VLESS/Reality**, **VMess**, **Trojan**, **Hysteria2**) and **WireGuard** built with **Go 1.24** and **React** (Vite/TS). Distributed as a single binary with two execution modes: bare metal and Docker on the same host. Designed for zero-downtime deployments via **Blue-Green pipelines** with health-checked watchdogs and atomic rollbacks.

<img width="3024" height="1714" alt="image" src="https://github.com/user-attachments/assets/9fca6d02-1f95-406b-a59e-0127b2c693ae" />

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

|           | Reality | TLS | None | TCP | WebSocket | HTTP | gRPC | HTTP Upgrade | UDP |
|-----------|:-------:|:---:|:----:|:---:|:---------:|:----:|:----:|:------------:|:---:|
| VLESS     | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| VMess     | — | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| Trojan    | — | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — |
| Hysteria2 | — | ✓ | — | — | — | — | — | — | ✓ |
| WireGuard | — | — | — | — | — | — | — | — | ✓ |

*   **Inbound Management**: create and edit managed Sing-box inbounds for VLESS, VMess, Trojan, and Hysteria2 from the UI, including Reality, WebSocket, and protocol-specific field validation.
*   **User Management**: create, edit, and delete Sing-box users per inbound; supports multiple inbound assignments, traffic limits, expiry dates, and Hysteria2 password-based users.
*   **Traffic Monitoring**: per-user download/upload stats for Sing-box inbounds; per-peer rx/tx stats across all WireGuard interfaces.
*   **WireGuard Interfaces**: create, configure, enable/disable, and delete WireGuard interfaces; raw `.conf` editing per interface.
*   **Service Control**: restart/stop Sing-box and WireGuard services and reload configurations without leaving the UI.
*   **Logs**: tail and filter Sing-box access logs and systemd journal entries in real time.
*   **Sysctl**: view and update whitelisted kernel parameters (e.g. `net.ipv4.ip_forward`) directly from the panel.
*   **Raw Configuration**: full JSON editor for the Sing-box config file (Experimental).

### Hysteria2 Notes

Current Hysteria2 support includes:

*   TLS-required inbound editing
*   Password-based user management
*   Optional `up_mbps` / `down_mbps` bandwidth limits
*   Optional `salamander` obfuscation

Hysteria2 QR/link generation is not documented as supported yet.

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
volumes:
  - /etc/sing-box:/etc/sing-box
  - /etc/wireguard:/etc/wireguard
  - /run/dbus:/run/dbus
  - /run/systemd:/run/systemd
  - /var/log/journal:/var/log/journal:ro
  - /run/log/journal:/run/log/journal:ro
```

Ready-to-use compose at [`docker/docker-local/docker-compose.yml`](docker/docker-local/docker-compose.yml).

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
  "api_key": "CHANGE_ME",
  "database_path": "./data/stats.db",

  "singbox_config_path": "/etc/sing-box/config.json",
  "singbox_api_addr": "127.0.0.1:8080",
  "managed_inbounds": ["in-reality", "in-reality-2"],
  "log_source": "journal",
  "access_log_path": "/var/log/singbox.log",

  "wireguard_config_path": "/etc/wireguard/wg0.conf",

  "execution_mode": "",

  "sysctl_whitelist": [
    "net.ipv4.ip_forward",
    "net.ipv6.conf.all.forwarding"
  ]
}
```

## CI/CD

Production-ready **Blue/Green deployment** via GitHub Actions: dual-slot topology (blue/green) managed by a watchdog, atomic Nginx reload on promotion, configurable baking window, and automatic rollback on health degradation.

**[Full setup guide → DEPLOY_GITHUB_ACTIONS.md](DEPLOY_GITHUB_ACTIONS.md)**

## License

MIT
