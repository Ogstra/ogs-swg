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

## Security Setup (Target Host)

For **Remote Mode** to function securely (which is what the CI/CD pipeline deploys), a restricted user account must be configured on the VPN node. Do not use the root account directly.

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
