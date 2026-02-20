# OGS-SWG Panel

A web-based control panel for managing Sing-box users (VLESS/Reality) and monitoring WireGuard traffic. This system supports both local execution (bare-metal) and remote management via SSH (Docker/AWS).

![Dashboard Screenshot](https://github.com/user-attachments/assets/db59dedb-9f6e-4a70-8421-756fb7156a12)

## Architecture

The application utilizes a modular System Executor architecture:
*   **Local Mode**: Executes commands (`systemctl`, `wg`, `ip`) directly on the host. Ideal for bare-metal installations on Debian/Ubuntu.
*   **Remote Mode (SSH)**: Connects to a remote host via SSH to manage services and retrieve logs. Suitable for containerized environments (Docker, AWS ECS/Fargate) or distributed architectures.

## Quick Start (Docker)

To run the application locally using Docker:

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/Ogstra/ogs-swg
    cd ogs-swg
    ```

2.  **Start the container**:
    ```bash
    docker compose up -d
    ```

3.  **Access the Dashboard**:
    Open `http://localhost:8080`.

---

## Configuration

Configuration is managed via `config.json` (mapped to `/app/data` in Docker) and environment variables.

### Bootstrap Admin

On the first run, if no admin user exists, you must set the following environment variables to create one:
```yaml
environment:
  - OGS_ADMIN_USER=admin
  - OGS_ADMIN_PASSWORD=your_strong_password
```
*(Add these to your `docker-compose.yml` or export them for bare metal execution)*

### Main Config (`config.json`)

Manage API keys, SSH targets, and file paths. The system automatically detects whether to run in Local or Remote mode based on the `ssh_host` parameter.

*   **Local Mode**: Leave `ssh_host` empty (`""`). The application will execute commands directly on the host machine.
*   **Remote Mode**: Provide the `ssh_host` and corresponding SSH configuration.

```json
{
  "listen_addr": ":8080",
  "api_key": "CHANGE_ME_STRONG_RANDOM_API_KEY",
  "database_path": "./data/stats.db",
  
  "singbox_config_path": "/etc/sing-box/config.json",
  "singbox_api_addr": "127.0.0.1:8080",
  "managed_inbounds": ["in-reality", "in-reality-8443"],
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
*(For Local mode, you can safely remove all the `ssh_*` keys or leave them empty).*

---

## CI/CD Pipeline Architecture

This repository includes a production-ready **Blue/Green Deployment** pipeline using GitHub Actions suitable for zero-downtime updates.

### Deployment Strategy

The system employs a dual-slot topology (Blue/Green) managed by a watchdog process:
1.  **Nginx Proxy**: Routes traffic to the currently active slot.
2.  **Atomic Switch**: The pipeline deploys the new version to the inactive slot, validates health, and performs an atomic reload of Nginx.
3.  **Baking Window**: The old slot remains active for a configurable duration (default 10 minutes) to handle in-flight connections.
4.  **Automatic Rollback**: A watchdog monitors the health of the active slot. If degradation is detected, it automatically reverts traffic to the previous stable slot.

### Remote Agent Security

The deployment relies on a restricted SSH user (`ogs_agent`) on the target host. This user operates with least-privilege `sudo` permissions, allowing control only over specific service units (`sing-box`, `wg-quick`) and configuration files.

For detailed setup instructions, including required secrets, `sudoers` configuration, and host hardening steps, please refer to the deployment guide:

**[Read the GitHub Actions Deployment Guide (DEPLOY_GITHUB_ACTIONS.md)](DEPLOY_GITHUB_ACTIONS.md)**

---

## Features

### Core Capabilities
*   **User Management**: create, modify, and delete VLESS/Reality users with multi-inbound support.
*   **Traffic Monitoring**: real-time traffic statistics for Sing-box users and WireGuard peers.
*   **Service Control**: restart/stop services and reload configurations directly from the UI.
*   **Logs**: view and filter system logs (Sing-box/Systemd).

### Supported Protocols
*   **VLESS** (Reality/TLS)
*   **VMess** (TLS/None)
*   **Trojan** (TLS)
*   **WireGuard**

### System Management
*   **Sysctl Management**: view and edit allowed kernel parameters via whitelist.
*   **Self-Signed Certificates**: generate ephemeral certificates for internal testing.
*   **Raw Configuration**: direct editing of underlying JSON configurations (Experimental).

## License

MIT
