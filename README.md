# OGS-SWG Panel

A web-based control panel for managing Sing-box users (VLESS/Reality) and monitoring WireGuard traffic. This system supports both **local execution** (bare-metal) and **remote management** via SSH (Docker/AWS).

<img width="1508" height="849" alt="Screenshot at Feb 03 18-30-39" src="https://github.com/user-attachments/assets/db59dedb-9f6e-4a70-8421-756fb7156a12" />

## Architecture

This application utilizes a modular **System Executor** architecture:

*   **Local Mode**: Executes commands (`systemctl`, `wg`, `ip`) directly on the host. Ideal for bare-metal installations on Debian/Ubuntu.
*   **Remote Mode (SSH)**: Connects to a remote host via SSH to manage services and retrieve logs. Suitable for containerized environments (Docker, AWS ECS/Fargate) or distributed architectures.

---

## Deployment

### Option 1: Docker (Recommended)

1.  **Clone the repository**:
    ```bash
    git clone https://github.com/Ogstra/ogs-swg
    cd ogs-swg
    ```

2.  **Prepare SSH Keys** (for Remote Mode):
    Generate a dedicated keypair for the agent:
    ```bash
    ssh-keygen -t ed25519 -f config/ssh_key -C "ogs_agent" -N ""
    ```
    *Copy the public key (`config/ssh_key.pub`) to the target server's `~/.ssh/authorized_keys`.*
    
    Add target host key pinning file:
    ```bash
    ssh-keyscan -H YOUR_SERVER_HOST > config/known_hosts
    ```

3.  **Run with Docker Compose**:
    ```yaml
    services:
      ogs-swg:
        build: .
        ports:
          - "8080:8080"
        volumes:
          - ./data:/app/data
          - ./config/ssh_key:/app/secrets/ssh_key:ro
          - ./config/known_hosts:/app/secrets/known_hosts:ro
          # Mount host configs for direct management
          - /etc/sing-box:/etc/sing-box
          - /etc/wireguard:/etc/wireguard
        environment:
          - OGS_LISTEN_ADDR=:8080
          - OGS_DB_PATH=/app/data/stats.db
          - OGS_SSH_KEY_PATH=/app/secrets/ssh_key
          - OGS_SSH_KNOWN_HOSTS=/app/secrets/known_hosts
          - OGS_ADMIN_USER=admin
          - OGS_ADMIN_PASSWORD=CHANGE_ME_STRONG_PASSWORD
    ```
    
    Start the container:
    ```bash
    docker compose up -d
    ```

### GitHub Actions Deploy Strategy

Automatic deploys use a **Blue/Green topology** only in CI/CD (not for local developers):

- Local users keep using the standard `docker-compose.yml`.
- CI deploy uses dedicated files under `docker/bluegreen/`.
- `nginx` routes traffic to active slot (`blue` or `green`) on an internal Docker network.
- Workflow deploys the inactive slot, runs healthchecks, and only then switches proxy upstream.
- If healthcheck fails, traffic stays on previous slot.

Required GitHub secrets for deploy:
- `DOCKER_USERNAME`
- `DOCKER_PASSWORD`
- `VPS_HOST`
- `VPS_PORT`
- `VPS_USER`
- `VPS_SSH_KEY` (deployment SSH key for Actions -> VPS)
- `OGS_AGENT_SSH_KEY` (app runtime key for panel -> managed node)
- `OGS_SSH_KNOWN_HOSTS_CONTENT` (known_hosts content for runtime SSH trust)
- `OGS_AGENT_USER`
- `OGS_API_KEY` (recommended; used by deploy to validate `/api/diag/ssh`)
- `OGS_ADMIN_USER` and `OGS_ADMIN_PASSWORD` (optional fallback only if `OGS_API_KEY` is not set)
- `OGS_PORT` (optional, defaults to `8080`)

Manual force deploy:
- In **Actions > Build and Deploy > Run workflow**, use `force_slot`:
  - `auto`: normal blue/green toggle
  - `blue`: force deploy to blue slot
  - `green`: force deploy to green slot

### Option 2: Bare Metal

Build and run the application directly:
```bash
go build -o ogs-swg ./cmd/server
./ogs-swg -config config.json
```

---

## Security Setup (Target Host)

For **Remote Mode** to function securely, a restricted user account must be configured on the VPN node. Do not use the root account directly.

### 1. Create the Agent User

On your VPN server (Target Host):
```bash
sudo useradd -m -s /bin/bash ogs_agent
```

### 2. Configure SSH Access

Add the public key generated during the deployment step:
```bash
sudo mkdir -p /home/ogs_agent/.ssh
echo "YOUR_PUBLIC_KEY_CONTENT" | sudo tee -a /home/ogs_agent/.ssh/authorized_keys
sudo chown -R ogs_agent:ogs_agent /home/ogs_agent/.ssh
sudo chmod 700 /home/ogs_agent/.ssh
sudo chmod 600 /home/ogs_agent/.ssh/authorized_keys
```

### 3. Configure Sudo Privileges (Least Privilege)

The application requires limited `sudo` access to manage services. Create a sudoers file:

```bash
sudo visudo -f /etc/sudoers.d/ogs_agent
```

Add the following configuration (verify paths for your distribution):

```sudoers
# Allow ogs_agent to manage specific services and files without password
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl restart sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl stop sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl start sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl is-active sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl restart wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl stop wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl start wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl is-active wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/wg show
ogs_agent ALL=(root) NOPASSWD: /usr/bin/wg syncconf *
ogs_agent ALL=(root) NOPASSWD: /usr/sbin/sysctl -w *
ogs_agent ALL=(root) NOPASSWD: /usr/sbin/sysctl -n *
ogs_agent ALL=(root) NOPASSWD: /usr/bin/journalctl -u sing-box *
```

*Note: The sysctl and wg patterns use wildcards but are validated strictly within the application code via a whitelist.*

---

## Configuration

Configure the application via `config.json` or the Settings UI:

```json
{
  "api_key": "CHANGE_ME_STRONG_RANDOM_API_KEY",
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

## Bootstrap Admin

On first run, if no admin exists in DB, you must provide:

```bash
export OGS_ADMIN_USER=admin
export OGS_ADMIN_PASSWORD='use-a-strong-random-password'
```

Without `OGS_ADMIN_PASSWORD`, startup is intentionally blocked to avoid insecure default credentials.

If you later remove admin users for operational reasons, keep `api_key` configured and set `OGS_API_KEY` in GitHub Actions secrets so deploy diagnostics can still authenticate.

## Supported Protocols

| Protocol | Security/Mode | QR/Link | Notes |
| --- | --- | --- | --- |
| VLESS | Reality / TLS / None | Yes | Uses Reality when present; falls back to standard VLESS |
| VMess | TLS / None | Yes | Uses `vmess_security` and `alter_id` from metadata |
| Trojan | TLS | Yes | TLS + transport supported |

## Features

**Stable**
- Real-time traffic monitoring
- Multi-inbound user management for Sing-box (add/edit/remove per inbound)
- WireGuard peer management
- QR/link generation per inbound (VLESS/VMess/Trojan) and WireGuard
- Sing-box log viewer with filtering
- Service control (start/stop/restart)
- Dashboard preferences (default service, refresh interval, range)
- **Sysctl Management** (View/Edit allowed keys)

**Experimental**
- VMess/Trojan inbound creation (Sing-box validation still required)
- Self-signed TLS certificate generator
- Raw configuration editor

## License

MIT
