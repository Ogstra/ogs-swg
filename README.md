# OGS-XWG

OGS-XWG is a lightweight, modern web interface for managing sing-box and WireGuard VPN services. It provides a real-time dashboard, user management, and configuration editing capabilities.

## Features

- **Dashboard**: Real-time traffic monitoring, service status indicators (sing-box & WireGuard), and historical data visualization.
- **User Management**: Create, update, and delete users. Bulk generation supported.
- **WireGuard Integration**: Manage WireGuard peers and view client configurations (QR code & text).
- **System Logs**: View real-time logs from sing-box (journal or file).
- **Settings**:
  - Edit raw `config.json` for sing-box.
  - Edit raw `wg0.conf` for WireGuard.
  - Restart/Stop services directly from the panel.
- **Responsive Design**: Works great on desktop and mobile devices.

## Installation

### Prerequisites

- Go 1.21+
- Node.js 18+ (for building frontend)
- sing-box installed (with `with_v2ray_api` build tag)
- WireGuard installed (`wireguard-tools`)

### Build & Run

1.  **Clone the repository:**
    ```bash
    git clone https://github.com/yourusername/xpanel.git
    cd xpanel
    ```

2.  **Build Frontend:**
    ```bash
    cd frontend
    npm install
    npm run build
    cd ..
    ```

3.  **Build Backend:**
    ```bash
    go mod tidy
    go build -o xpanel
    ```

4.  **Configuration:**
    Create a `config.json` in the root directory (or rely on defaults):
    ```json
    {
      "singbox_config_path": "/etc/sing-box/config.json",
      "singbox_api_addr": "127.0.0.1:8080",
      "managed_inbounds": ["in-reality"],
      "stats_inbounds": ["in-reality"],
      "stats_outbounds": ["direct"],
      "access_log_path": "/var/log/singbox.log",
      "log_source": "journal",
      "database_path": "./stats.db",
      "listen_addr": ":8080",
      "wireguard_config_path": "/etc/wireguard/wg0.conf",
      "enable_wireguard": true,
      "enable_singbox": true,
      "use_stats_sampler": true,
      "sampler_interval_sec": 60,
      "api_key": "change-me"
    }
    ```

5.  **Run:**
    ```bash
    ./xpanel
    ```

## Docker

You can also run OGS-XWG using Docker:

```bash
docker-compose up -d
```

Notes:
- In Docker, journalctl is not available; set `log_source` to `file` and bind-mount a log file (e.g. `/var/log/singbox.log`) as shown in `docker-compose.yml`.

## License

MIT
=======
# ogs-swg
Lightweight web dashboard to monitor and manage sing-box and WireGuard with real-time stats, user controls, and config editing
