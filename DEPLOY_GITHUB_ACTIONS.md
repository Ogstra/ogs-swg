# OGS-SWG Deployment Guide (GitHub Actions)

The current pipeline deploys the panel in **docker_local** mode only:
- GitHub Actions connects to the VPS over SSH (transport only).
- The app itself manages sing-box/wireguard on the same host through Docker bind mounts + systemd runtime sockets.

## Required Secrets

| Secret | Required | Notes |
|--------|----------|-------|
| `DOCKER_USERNAME` | Yes | Docker Hub namespace |
| `DOCKER_PASSWORD` | Yes | Docker Hub token/password |
| `VPS_HOST` | Yes | Target host |
| `VPS_PORT` | Yes | SSH port |
| `VPS_USER` | Yes | SSH user used by Actions on VPS |
| `VPS_SSH_KEY` | Yes | Deployment key for Actions -> VPS |
| `DEPLOY_PATH` | Yes | Base path where blue/green files are stored |
| `OGS_API_KEY` | Recommended | API key injected into app slots |
| `OGS_ADMIN_USER` / `OGS_ADMIN_PASSWORD` | Optional | First-run bootstrap/fallback auth |
| `OGS_PORT` | Optional | Public proxy port (default `8080`) |
| `DEPLOY_ARCH` | Optional | Docker buildx target (default `linux/amd64,linux/arm64`) |

## Workflow Inputs

In Actions -> Build and Deploy -> Run workflow:

| Input | Values | Default |
|-------|--------|---------|
| `force_slot` | `auto` / `blue` / `green` | `auto` |
| `bake_seconds` | `60..86400` | `600` |

## Host Prerequisites (docker_local)

On the VPS where containers run:

1. Docker + Compose plugin installed.
2. `sing-box` managed by systemd.
3. `wg-quick@wg0` managed by systemd.
4. Directories exist:
   - `/etc/sing-box`
   - `/etc/wireguard`
5. Systemd runtime paths available for mounts:
   - `/run/dbus`
   - `/run/systemd`
   - `/var/log/journal`
   - `/run/log/journal`

The deployed slot containers use these mounts to perform service/log/sysctl/wg operations from inside Docker.

## Runtime State Files

Location: `${DEPLOY_PATH}` on VPS.

| File | Purpose |
|------|---------|
| `.bluegreen.active` | Current live slot |
| `.bluegreen.previous` | Previous slot used for rollback |
| `.bluegreen.bake_until` | Unix timestamp; old slot is stopped after this |
| `.bluegreen.events.log` | Watchdog events and recovered incidents |

## Notes

- Runtime SSH executor support was removed from the application.
- Secrets like `OGS_AGENT_SSH_KEY_B64`, `OGS_AGENT_USER`, and `OGS_SSH_KNOWN_HOSTS_CONTENT*` are no longer used.
- Health checks validate slots via `GET /health` expecting `204`.
