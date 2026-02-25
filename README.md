# OGS-SWG Panel

Unified control plane for **Sing-box** (**VLESS/Reality**) and **WireGuard** built with **Go 1.24** and **React** (Vite/TS). Distributed as a single binary with two execution modes: bare metal and Docker on the same host. Designed for zero-downtime deployments via **Blue-Green pipelines** with health-checked watchdogs and atomic rollbacks.

![Dashboard Screenshot](https://github.com/user-attachments/assets/db59dedb-9f6e-4a70-8421-756fb7156a12)

## Tech Stack
*   **Runtime**: Go 1.24
*   **Frontend**: React 19 + TypeScript (Vite)
*   **DB**: SQLite + [sqlc](https://sqlc.dev/) (Type-safe SQL)
*   **Cache**: [Ristretto](https://github.com/dgraph-io/ristretto)
*   **Concurrency**: [Pond](https://github.com/alitto/pond) (Goroutine worker pools)
*   **Config**: [Cleanenv](https://github.com/ilyakaznacheev/cleanenv) (`.env` + JSON)
*   **Validation**: [Validator v10](https://github.com/go-playground/validator)

## Features

| Protocol | Security | Transport |
|----------|----------|-----------|
| VLESS    | Reality / None | TCP |
| VMess    | TLS / None | TCP · WebSocket |
| Trojan   | TLS | TCP |
| WireGuard | WireGuard | UDP |

*   **User Management**: create, modify, and delete users with multi-inbound support.
*   **Traffic Monitoring**: real-time stats for Sing-box users and WireGuard peers.
*   **Service Control**: restart/stop services and reload configs from the UI.
*   **Logs**: view and filter system logs (Sing-box/Systemd).
*   **Sysctl Management**: view and edit allowed kernel parameters via whitelist.
*   **Raw Configuration**: direct editing of underlying JSON configurations (Experimental).

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
