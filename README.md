# OGS-SWG Panel

Unified control plane for **Sing-box** (**VLESS/Reality**) and **WireGuard** built with **Go 1.24** and **React** (Vite/TS). Distributed as a **single binary** with native support for **local execution** and **remote orchestration via SSH**. Designed for high-availability deployment cycles using automated **Blue-Green pipelines** with health-checked watchdogs and atomic rollbacks.

![Dashboard Screenshot](https://github.com/user-attachments/assets/db59dedb-9f6e-4a70-8421-756fb7156a12)

## Features

### Supported Protocols

| Protocol | Transport |
|----------|-----------|
| VLESS    | Reality / TLS |
| VMess    | TLS / None |
| Trojan   | TLS |
| WireGuard | UDP |

### Core Capabilities
*   **User Management**: create, modify, and delete users with multi-inbound support.
*   **Traffic Monitoring**: real-time stats for Sing-box users and WireGuard peers.
*   **Service Control**: restart/stop services and reload configs from the UI.
*   **Logs**: view and filter system logs (Sing-box/Systemd).

### System Management
*   **Sysctl Management**: view and edit allowed kernel parameters via whitelist.
*   **Self-Signed Certificates**: generate ephemeral certificates for internal testing.
*   **Raw Configuration**: direct editing of underlying JSON configurations (Experimental).

## Architecture

| Mode | Trigger | Executor |
|------|---------|----------|
| **Local** | `ssh_host` empty | Runs `systemctl`, `wg`, `ip` directly on host |
| **Remote** | `ssh_host` set | Tunnels commands over SSH (Docker, ECS/Fargate) |

## Tech Stack
*   **Runtime**: Go 1.24
*   **Frontend**: React 18 + TypeScript (Vite)
*   **DB**: SQLite + [sqlc](https://sqlc.dev/) (Type-safe SQL)
*   **Cache**: [Ristretto](https://github.com/dgraph-io/ristretto)
*   **Concurrency**: [Pond](https://github.com/alitto/pond) (Goroutine worker pools)
*   **Config**: [Cleanenv](https://github.com/ilyakaznacheev/cleanenv) (`.env` + JSON)
*   **Validation**: [Validator v10](https://github.com/go-playground/validator)

## Quick Start (Docker)

```bash
git clone https://github.com/Ogstra/ogs-swg && cd ogs-swg
docker compose up -d
```

---

### Bootstrap Admin

On the first run, if no admin user exists, you must set the following environment variables to create one:
```yaml
environment:
  - OGS_ADMIN_USER=admin
  - OGS_ADMIN_PASSWORD=admin
```
*(Add these to your `docker-compose.yml` or export them for bare metal execution)*

### Main Config (`config.json` / `.env`)

Configuration parameters are merged from environment variables, `.env`, and `config.json`. The application automatically detects Local/Remote mode based on `ssh_host`.

*   **Local Mode**: Leave `ssh_host` empty (`""`). The application will execute commands directly on the host machine.
*   **Remote Mode**: Provide the `ssh_host` and corresponding SSH configuration.

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

  "ssh_host": "10.0.0.5", 
  "ssh_port": 22,
  "ssh_user": "ogs_agent",
  "ssh_key_path": "/app/secrets/ssh_key",
  "ssh_known_hosts_path": "/app/secrets/known_hosts",

  "sysctl_whitelist": [
    "net.ipv4.ip_forward",
    "net.ipv6.conf.all.forwarding"
  ]
}
```

## CI/CD Pipeline Architecture

This repository includes a production-ready **Blue/Green Deployment** pipeline using GitHub Actions suitable for zero-downtime updates.

### Deployment Strategy

The system employs a dual-slot topology (Blue/Green) managed by a watchdog process:
1.  **Nginx Proxy**: Routes traffic to the currently active slot.
2.  **Atomic Switch**: The pipeline deploys the new version to the inactive slot, validates health, and performs an atomic reload of Nginx.
3.  **Baking Window**: The old slot remains active for a configurable duration (default 10 minutes) to handle in-flight connections.
4.  **Automatic Rollback**: A watchdog monitors the health of the active slot. If degradation is detected, it automatically reverts traffic to the previous stable slot.

For detailed setup instructions, including required secrets, `sudoers` configuration, and host hardening steps, please refer to the deployment guide:

**[GitHub Actions Deployment Guide (DEPLOY_GITHUB_ACTIONS.md)](DEPLOY_GITHUB_ACTIONS.md)**

## License

MIT
