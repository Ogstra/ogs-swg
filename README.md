# OGS-SWG

Web dashboard for managing sing-box and WireGuard VPN services.

<img width="1499" height="851" alt="image" src="https://github.com/user-attachments/assets/b16a6c51-849d-4685-817b-9b3acf59a4ba" />

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

**Experimental**
- VMess/Trojan inbound creation (sing-box validation still required)
- Self-signed TLS certificate generator (Tools)
- Raw configuration editor with find + backup/restore
## Installation

### Requirements

- Go 1.24+
- Node.js 18+
- sing-box with V2Ray API support, build with tags `with_v2ray_api`, `with_quic`, `with_dhcp`, `with_wireguard`, `with_utls`, `with_acme`, `with_clash_api`, `with_gvisor`
- WireGuard tools (`wg`, `wg-quick`) 

### Build

```bash
git clone https://github.com/yourusername/ogs-swg.git
cd ogs-swg

# Frontend
cd frontend
npm install
npm run build
cd ..

# Backend
go build -o ogs-swg .
```

### Configuration

Create `config.json`:

```json
{
  "singbox_config_path": "/etc/sing-box/config.json",
  "singbox_api_addr": "127.0.0.1:8080",
  "managed_inbounds": [],
  "stats_inbounds": [],
  "stats_outbounds": [],
  "access_log_path": "/var/log/singbox.log",
  "log_source": "journal",
  "database_path": "./stats.db",
  "listen_addr": "0.0.0.0:8111",
  "wireguard_config_path": "/etc/wireguard/wg0.conf",
  "enable_wireguard": true,
  "enable_singbox": true,
  "use_stats_sampler": true,
  "sampler_interval_sec": 60,
  "active_threshold_bytes": 1024,
  "retention_enabled": true,
  "retention_days": 30,
  "wg_sampler_interval_sec": 60,
  "wg_retention_days": 30,
  "aggregation_enabled": true,
  "aggregation_days": 7,
  "public_ip": ""
}
```

**Required:**
- `singbox_config_path` - Path to sing-box configuration file
- `singbox_api_addr` - sing-box API listen address
- `database_path` - SQLite database path for stats
- `listen_addr` - Dashboard web server listen address

**Optional:**
- `managed_inbounds` - Inbound tags to manage (default: `[]`)
- `stats_inbounds` - Inbound tags to collect stats from (default: `[]`)
- `stats_outbounds` - Outbound tags to collect stats from (default: `[]`)
- `access_log_path` - Path to sing-box log file (default: `/var/log/singbox.log`)
- `log_source` - Log source: `"journal"` or `"file"` (default: `"journal"`)
- `wireguard_config_path` - Path to WireGuard config (default: `/etc/wireguard/wg0.conf`)
- `enable_wireguard` - Enable WireGuard management (default: `true`)
- `enable_singbox` - Enable sing-box management (default: `true`)
- `use_stats_sampler` - Enable traffic statistics sampling (default: `true`)
- `sampler_interval_sec` - Stats sampling interval in seconds (default: `60`)
- `active_threshold_bytes` - Minimum bytes to consider user active (default: `1024`)
- `retention_enabled` - Enable automatic data cleanup (default: `true`)
- `retention_days` - Days to keep raw stats data (default: `30`)
- `wg_sampler_interval_sec` - WireGuard stats sampling interval (default: `60`)
- `wg_retention_days` - Days to keep WireGuard stats (default: `30`)
- `aggregation_enabled` - Enable data aggregation (default: `true`)
- `aggregation_days` - Days threshold for aggregation (default: `7`)
- `public_ip` - Public IP used for QR/link generation (falls back to request host if empty)
### Run

```bash
./ogs-swg
```

Access at `http://localhost:PORT`

Default login: `admin` / `admin`

## Docker (EXPERIMENTAL)

```bash
docker-compose up -d
```

**Important notes for Docker:**

- Set `log_source: "file"` instead of `"journal"` (systemd not available in containers)
- Bind-mount your sing-box log file if using file-based logs
- The default compose uses `network_mode: host` for WireGuard access
- Requires `NET_ADMIN` capability and `/dev/net/tun` device access
- Service control (start/stop/restart) may not work without systemd

**Example Docker configuration:**

```json
{
  "singbox_config_path": "/etc/sing-box/config.json",
  "singbox_api_addr": "127.0.0.1:8080",
  "log_source": "file",
  "access_log_path": "/var/log/singbox.log",
  "database_path": "/data/stats.db",
  "listen_addr": ":8111"
}
```

## License

MIT
