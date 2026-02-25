# OGS-SWG GitHub Actions Deployment Guide

This guide details how to set up the CI/CD pipeline and the necessary security configurations on your target host.

## CI/CD Blue-Green Deploy (GitHub Actions)

Automatic deploys use a **blue/green topology with watchdog**:

- Local development keeps using `docker-compose.yml`.
- CI uses `docker/bluegreen/*`.
- `nginx` routes traffic to active slot (`blue` or `green`).
- Deploy pipeline updates the inactive slot, validates health + `/api/diag/ssh`, then does an atomic nginx reload.
- Old slot stays alive during a baking window (default `600s`), then watchdog stops it to recover RAM.
- If active slot degrades later, watchdog can auto-rollback to the previous slot and logs the incident.
- Image deploy is immutable per run (`ogs-swg:${GITHUB_SHA}`), while `latest` is still pushed for convenience.

### Required Actions Secrets

Configure these secrets in your GitHub repository settings:

- `DOCKER_USERNAME`
- `DOCKER_PASSWORD`
- `VPS_HOST`
- `VPS_PORT`
- `VPS_USER`
- `VPS_SSH_KEY` (deployment SSH key for Actions -> VPS)
- `OGS_AGENT_SSH_KEY_B64` (base64-encoded runtime private key; required)
- `OGS_SSH_KNOWN_HOSTS_CONTENT` (known_hosts content for runtime SSH trust)
- `OGS_SSH_KNOWN_HOSTS_CONTENT_B64` (optional; base64 of known_hosts content, preferred over multiline secret)
- `OGS_AGENT_USER`
- `OGS_API_KEY` (recommended; used by deploy to validate `/api/diag/ssh`)
- `OGS_ADMIN_USER` and `OGS_ADMIN_PASSWORD` (optional fallback only if `OGS_API_KEY` is not set)
- `OGS_PORT` (optional, defaults to `8080`)

### Deploy Behavior

- If `OGS_AGENT_USER` is not `root`, the workflow provisions `/etc/sudoers.d/ogs-swg-<user>` automatically on each deploy.
- The file is replaced (idempotent), so rules are not duplicated.
- This requires `${VPS_USER}` to be `root` or have passwordless sudo for `visudo`/`install` to `/etc/sudoers.d`.
- If `OGS_AGENT_USER=root`, sudoers provisioning is skipped.
- Workflow also syncs the runtime SSH public key (derived from `OGS_AGENT_SSH_KEY_B64`) into `${OGS_AGENT_USER}` `authorized_keys` on each deploy.

### Manual Deploy Control

Inputs for `force_slot` in Actions > Build and Deploy > Run workflow:
- `auto`: normal toggle
- `blue`: force deploy to blue
- `green`: force deploy to green

**Optional Input:**
- `bake_seconds`: baking window duration before watchdog stops old slot (range `60..86400`, default `600`).

### Runtime State Files

Location: `${DEPLOY_PATH}` on VPS.
- `.bluegreen.active`: current live slot.
- `.bluegreen.previous`: previous slot used for rollback.
- `.bluegreen.bake_until`: unix timestamp; when elapsed, old slot is stopped.
- `.bluegreen.events.log`: watchdog events and recovered incidents.

### Memory Usage
Stable target after baking: `1 app active + 1 watchdog + 1 nginx proxy`.

---

## Docker Local Mode

Use this mode when the panel runs as a Docker container **on the same host** as the singbox and wg-quick systemd services. No SSH connection is needed — the container uses `nsenter -t 1` to reach host namespaces, and bind mounts expose service config files at the same paths.

### Container requirements

| Requirement | Compose key | Purpose |
|-------------|------------|---------|
| Host PID namespace | `pid: host` | `nsenter -t 1` targets the host init process |
| `SYS_PTRACE` capability | `cap_add: [SYS_PTRACE]` | Required to enter host namespaces via nsenter |
| `SYS_ADMIN` capability | `cap_add: [SYS_ADMIN]` | Required for sysctl writes |
| Bind mounts | `volumes` | `/etc/sing-box` and `/etc/wireguard` at the same host paths |

### Standalone deployment

```bash
cd docker/docker-local
cp ../../config.json.example ../../data/config.json
# Edit data/config.json — set execution_mode to "docker_local" (already set via env var)
OGS_API_KEY=changeme docker compose up -d
```

The compose file at `docker/docker-local/docker-compose.yml` sets `OGS_EXECUTION_MODE=docker_local` automatically.

### Blue/Green deployment (local mode)

Use `docker-compose.blue-local.yml` / `docker-compose.green-local.yml` instead of the SSH variants:

```bash
# Deploy blue slot
docker compose --env-file .env.bluegreen -f docker/bluegreen/docker-compose.blue-local.yml up -d
```

**Required `.env.bluegreen` keys** (SSH keys are NOT needed):

```env
OGS_IMAGE=yourusername/ogs-swg:<sha>
OGS_PROXY_HTTP_PORT=8080
OGS_API_KEY=<your-api-key>
OGS_ADMIN_USER=<admin>
OGS_ADMIN_PASSWORD=<password>
```

Keys **not required** for local mode: `OGS_SSH_HOST`, `OGS_SSH_PORT`, `OGS_SSH_USER`, `OGS_AGENT_SSH_KEY_B64`, `OGS_SSH_KNOWN_HOSTS_*`.

### GitHub Actions CI/CD (local mode)

The existing `deploy.yml` is designed for SSH mode. For Docker Local mode, the main differences are:

- **Skip** the SSH key setup steps (`OGS_AGENT_SSH_KEY_B64`, known_hosts, sudoers provisioning).
- **Skip** the `/api/diag/ssh` connectivity validation step (there is no SSH connection to validate).
- Use `docker-compose.*-local.yml` files instead of `docker-compose.blue.yml` / `docker-compose.green.yml`.
- Health validation still applies (the HTTP `401` login check is mode-agnostic).

A dedicated `deploy-local.yml` workflow can be set up with the following simplified secrets:

| Secret | Required |
|--------|----------|
| `DOCKER_USERNAME` | Yes |
| `DOCKER_PASSWORD` | Yes |
| `VPS_HOST` | Yes |
| `VPS_PORT` | Yes |
| `VPS_USER` | Yes |
| `VPS_SSH_KEY` | Yes (deployment key for Actions → VPS) |
| `OGS_API_KEY` | Yes |
| `OGS_PORT` | Optional (default `8080`) |
| `DEPLOY_ARCH` | Optional (default `linux/amd64,linux/arm64`) |

### No sudoers configuration needed

In Docker Local mode, privilege escalation is handled by Docker capabilities (`SYS_PTRACE`, `SYS_ADMIN`) rather than sudoers rules. No user account setup on the target host is required beyond Docker access.

---

## Security Setup (Target Host)

For **Remote Mode** to function securely (which is what the CI/CD pipeline deploys), a restricted user account must be configured on the server node. Do not use the root account directly.

### 1. Create the Agent User

On your server (Target Host):
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

*Note: The sysctl and wg patterns use wildcards but are validated strictly within the application code via a whitelist.*
*If your runtime SSH user is `root`, these sudoers entries are not required.*
*If you use this repository's GitHub Actions deploy, this sudoers file is provisioned automatically when `OGS_AGENT_USER` is not `root`.*
