# OGS-SWG Deployment Guide

## SSH Mode (Remote)

The panel runs in Docker and manages singbox/wg on a separate host over SSH. This is what the included `deploy.yml` pipeline uses.

### GitHub Actions Secrets

| Secret | Required | Notes |
|--------|----------|-------|
| `DOCKER_USERNAME` | Yes | |
| `DOCKER_PASSWORD` | Yes | |
| `VPS_HOST` | Yes | Target host |
| `VPS_PORT` | Yes | |
| `VPS_USER` | Yes | User with sudo on the VPS |
| `VPS_SSH_KEY` | Yes | Deployment key for Actions → VPS |
| `OGS_AGENT_SSH_KEY_B64` | Yes | Base64-encoded runtime private key |
| `OGS_AGENT_USER` | Yes | Runtime SSH user on the target host |
| `OGS_SSH_KNOWN_HOSTS_CONTENT_B64` | Yes | Base64 of known_hosts (preferred) |
| `OGS_SSH_KNOWN_HOSTS_CONTENT` | Fallback | Plaintext known_hosts if B64 not set |
| `OGS_API_KEY` | Recommended | Used by deploy to validate `/api/diag/ssh` |
| `OGS_ADMIN_USER` / `OGS_ADMIN_PASSWORD` | Optional | Fallback if `OGS_API_KEY` not set |
| `OGS_PORT` | Optional | Default `8080` |
| `DEPLOY_ARCH` | Optional | Default `linux/amd64,linux/arm64` |

### Deploy Behavior

- If `OGS_AGENT_USER` is not `root`, the workflow provisions `/etc/sudoers.d/ogs-swg-<user>` automatically on each deploy (idempotent).
- Requires `VPS_USER` to be `root` or have passwordless sudo for `visudo`/`install` to `/etc/sudoers.d`.
- Workflow syncs the runtime SSH public key into `OGS_AGENT_USER` `authorized_keys` on each deploy.

### Manual Deploy Control

In Actions → Build and Deploy → Run workflow:

| Input | Values | Default |
|-------|--------|---------|
| `force_slot` | `auto` / `blue` / `green` | `auto` |
| `bake_seconds` | `60..86400` | `600` |

### Runtime State Files

Location: `${DEPLOY_PATH}` on VPS.

| File | Purpose |
|------|---------|
| `.bluegreen.active` | Current live slot |
| `.bluegreen.previous` | Previous slot used for rollback |
| `.bluegreen.bake_until` | Unix timestamp; when elapsed, old slot is stopped |
| `.bluegreen.events.log` | Watchdog events and recovered incidents |

Stable memory target after baking: `1 app + 1 watchdog + 1 nginx`.

### Host Security Setup

Configure a restricted agent user on the target host. If `OGS_AGENT_USER=root`, skip this section — the pipeline handles it automatically for non-root users.

**1. Create the agent user:**
```bash
sudo useradd -m -s /bin/bash ogs_agent
```

**2. Add the SSH public key:**
```bash
sudo mkdir -p /home/ogs_agent/.ssh
echo "YOUR_PUBLIC_KEY" | sudo tee -a /home/ogs_agent/.ssh/authorized_keys
sudo chown -R ogs_agent:ogs_agent /home/ogs_agent/.ssh
sudo chmod 700 /home/ogs_agent/.ssh && sudo chmod 600 /home/ogs_agent/.ssh/authorized_keys
```

**3. Configure sudo (least privilege):**
```bash
sudo visudo -f /etc/sudoers.d/ogs_agent
```

```sudoers
Defaults:ogs_agent !requiretty
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl restart sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl stop sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl start sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl is-active sing-box
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl restart wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl stop wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl start wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/systemctl is-active wg-quick@wg0
ogs_agent ALL=(root) NOPASSWD: /usr/bin/wg show
ogs_agent ALL=(root) NOPASSWD: /usr/bin/wg show all dump
ogs_agent ALL=(root) NOPASSWD: /usr/bin/wg syncconf *
ogs_agent ALL=(root) NOPASSWD: /usr/bin/cat /etc/wireguard/wg0.conf
ogs_agent ALL=(root) NOPASSWD: /usr/bin/install -m * /tmp/ogs-swg-*.tmp /etc/wireguard/wg0.conf
ogs_agent ALL=(root) NOPASSWD: /usr/bin/cat /etc/sing-box/config.json
ogs_agent ALL=(root) NOPASSWD: /usr/bin/install -m * /tmp/ogs-swg-*.tmp /etc/sing-box/config.json
ogs_agent ALL=(root) NOPASSWD: /usr/sbin/sysctl -w *
ogs_agent ALL=(root) NOPASSWD: /usr/sbin/sysctl -n *
ogs_agent ALL=(root) NOPASSWD: /usr/bin/journalctl -u sing-box *
```

*Sysctl and wg wildcards are validated server-side via a strict whitelist.*

---

## Docker Local Mode

Panel runs as a Docker container on the **same host** as singbox and wg-quick (systemd). No SSH connection needed — file ops use bind mounts, system commands use `nsenter -t 1`.

### Container Requirements

| Compose key | Value | Purpose |
|-------------|-------|---------|
| `pid` | `host` | `nsenter -t 1` targets the host init process |
| `cap_add` | `SYS_PTRACE` | Enter host namespaces via nsenter |
| `cap_add` | `SYS_ADMIN` | sysctl writes |
| `volumes` | `/etc/sing-box:/etc/sing-box` | Bind-mount at same host path |
| `volumes` | `/etc/wireguard:/etc/wireguard` | Bind-mount at same host path |

### Standalone Deployment

```bash
cd docker/docker-local
OGS_API_KEY=changeme docker compose up -d
```

### Blue/Green Deployment

Use the `-local` compose variants instead of the SSH ones:

```bash
docker compose --env-file .env.bluegreen -f docker/bluegreen/docker-compose.blue-local.yml up -d
```

**`.env.bluegreen` for local mode:**

```env
OGS_IMAGE=yourusername/ogs-swg:<sha>
OGS_PROXY_HTTP_PORT=8080
OGS_API_KEY=<your-api-key>
OGS_ADMIN_USER=<admin>
OGS_ADMIN_PASSWORD=<password>
```

### GitHub Actions Secrets

For a `deploy-local.yml` workflow. SSH keys and sudoers are not needed.

| Secret | Required | Notes |
|--------|----------|-------|
| `DOCKER_USERNAME` | Yes | |
| `DOCKER_PASSWORD` | Yes | |
| `VPS_HOST` | Yes | |
| `VPS_PORT` | Yes | |
| `VPS_USER` | Yes | |
| `VPS_SSH_KEY` | Yes | Deployment key for Actions → VPS |
| `OGS_API_KEY` | Yes | |
| `OGS_EXECUTION_MODE` | Yes | Set to `docker_local` |
| `OGS_PORT` | Optional | Default `8080` |
| `DEPLOY_ARCH` | Optional | Default `linux/amd64,linux/arm64` |

Differences from SSH mode pipeline: skip SSH key setup, known_hosts, sudoers provisioning, and `/api/diag/ssh` validation. Health check (`401` on invalid login) still applies.
