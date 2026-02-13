# OGS-SWG Panel

A web-based control panel for managing Sing-box users (VLESS/Reality) and monitoring WireGuard traffic. Supports both **local execution** (bare-metal) and **remote management** via SSH (Docker/AWS).

## Architecture

This application uses a modular **System Executor** architecture:
*   **Local Mode**: Runs commands (`systemctl`, `wg`, `ip`) directly on the host. Suitable for bare-metal installs (Debian/Ubuntu).
*   **Remote Mode (SSH)**: Connects to a remote host via SSH to manage services and read logs. Suitable for **Docker**, **AWS ECS/Fargate**, or splitting the panel from the VPN node.

---

## 🚀 Deployment

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

3.  **Run with Docker Compose**:
    ```yaml
    services:
      ogs-swg:
        build: .
        ports:
          - "8080:8080"
        volumes:
          - ./data:/app/data
          - ./config/ssh_key:/app/data/ssh_key:ro
        environment:
          - OGS_DB_PATH=/app/data/stats.db
    ```


```bash
docker compose up -d
```

### Option 2: Bare Metal
Build and run directly:
```bash
go build -o ogs-swg ./cmd/server
./ogs-swg -config config.json
```

---

## 🔒 Security Setup (Target Host)

For the **Remote Mode** to work securely, you must create a restricted user on the VPN node. Do NOT use root directly.

### 1. Create the Agent User
On your VPN server (Target Host):
```bash
sudo useradd -m -s /bin/bash ogs_agent
```

### 2. Configure SSH Access
Add the public key generated in the deployment step:
```bash
sudo mkdir -p /home/ogs_agent/.ssh
echo "YOUR_PUBLIC_KEY_CONTENT" | sudo tee -a /home/ogs_agent/.ssh/authorized_keys
sudo chown -R ogs_agent:ogs_agent /home/ogs_agent/.ssh
sudo chmod 700 /home/ogs_agent/.ssh
sudo chmod 600 /home/ogs_agent/.ssh/authorized_keys
```

### 3. Configure Sudo Privileges (Least Privilege)
This application requires limited `sudo` access to manage services. Create a sudoers file:

```bash
sudo visudo -f /etc/sudoers.d/ogs_agent
```

Add the following configuration (replace `/usr/bin/wg` etc. with actual paths if different):

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

*Note: The sysctl and wg patterns use wildcards but are validated strictly within the application code (Whitelist).*

---

## Configuration

In `config.json` or via UI settings:

```json
{
  "ssh_host": "10.0.0.5",
  "ssh_port": 22,
  "ssh_user": "ogs_agent",
  "ssh_key_path": "/app/data/ssh_key",
  "sysctl_whitelist": [
    "net.ipv4.ip_forward",
    "net.ipv6.conf.all.forwarding"
  ]
}
```

## Supported protocols

| Protocol | Security/Mode | QR/Link | Notes |
| --- | --- | --- | --- |
| VLESS | Reality / TLS / None | Yes | Uses Reality when present; falls back to standard VLESS |
| VMess | TLS / None | Yes | Uses `vmess_security` and `alter_id` from metadata |
| Trojan | TLS | Yes | TLS + transport supported |

## Features

**Stable**
- Real-time traffic monitoring
- Multi-inbound user management for sing-box (add/edit/remove per inbound)
- WireGuard peer management
- QR/link generation per inbound (VLESS/VMess/Trojan) and WireGuard
- Sing-box log viewer with filtering
- Service control (start/stop/restart)
- Dashboard preferences (default service, refresh interval, range)
- **Sysctl Management** (View/Edit allowed keys)

**Experimental**
- VMess/Trojan inbound creation (sing-box validation still required)
- Self-signed TLS certificate generator (Tools)
- Raw configuration editor with find + backup/restore

## License

MIT
