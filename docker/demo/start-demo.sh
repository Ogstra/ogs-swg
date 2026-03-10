#!/usr/bin/env bash
set -euo pipefail

DATA_DIR="/app/data"
WG_DIR="$DATA_DIR/wireguard"
SINGBOX_DIR="$DATA_DIR/singbox"
CONFIG_PATH="$DATA_DIR/config.json"
ACCESS_LOG="$DATA_DIR/access.log"
DB_PATH="$DATA_DIR/stats.db"
FRONTEND_INDEX="/app/frontend/index.html"

# ── Data directory: wipe on every start (demo is stateless) ──────────────────
mkdir -p "$DATA_DIR"
find "$DATA_DIR" -mindepth 1 -maxdepth 1 -exec rm -rf {} +
mkdir -p "$WG_DIR" "$SINGBOX_DIR"

# ── Copy demo configs ─────────────────────────────────────────────────────────
cp /demo/config.demo.json "$CONFIG_PATH"
cp /demo/singbox.demo.json "$SINGBOX_DIR/config.json"
cp /demo/wireguard/*.conf "$WG_DIR/"
touch "$ACCESS_LOG"

# ── Environment ───────────────────────────────────────────────────────────────
export DEMO_DB_PATH="$DB_PATH"
export DEMO_LOG_PATH="$ACCESS_LOG"
export DEMO_API_KEY="${OGS_API_KEY:-demo-readonly-key}"
export OGS_ADMIN_USER="${OGS_ADMIN_USER:-demo-readonly}"
export OGS_ADMIN_PASSWORD="${OGS_ADMIN_PASSWORD:-demo-disabled-${RANDOM}${RANDOM}${RANDOM}}"
export OGS_DB_PATH="$DB_PATH"
export OGS_ACCESS_LOG_PATH="$ACCESS_LOG"
export OGS_LOG_SOURCE="file"
export OGS_SINGBOX_CONFIG_PATH="$SINGBOX_DIR/config.json"
export OGS_WIREGUARD_CONFIG_DIR="$WG_DIR"
export OGS_WIREGUARD_CONFIG_PATH="$WG_DIR/wg0.conf"
export OGS_WIREGUARD_TEST_MODE="true"
export OGS_DEMO_MODE="true"
export OGS_DISABLE_PASSWORD_LOGIN="true"

# ── Frontend: inject auto-login script before </head> ─────────────────────────
inject_demo_frontend_autologin() {
  [[ -f "$FRONTEND_INDEX" ]] || return 0

  local escaped_api_key="${DEMO_API_KEY//\\/\\\\}"
  escaped_api_key="${escaped_api_key//\"/\\\"}"

  local snippet tmp_index
  snippet="$(mktemp)"
  cat > "$snippet" <<SNIPPET
<!-- DEMO_AUTOLOGIN_START -->
<script>
(() => {
  const readonlyPerms = {
    can_read_users: true,    can_write_users: false,
    can_read_wireguard: true, can_write_wireguard: false,
    can_read_config: true,   can_write_config: false,
    can_read_settings: true, can_write_settings: false,
    can_read_panel_users: false, can_write_panel_users: false,
    can_read_logs: true
  };
  localStorage.setItem('api_key', "${escaped_api_key}");
  localStorage.setItem('permissions', JSON.stringify(readonlyPerms));
  localStorage.setItem('demo_mode', '1');
  localStorage.removeItem('token');
})();
</script>
<!-- DEMO_AUTOLOGIN_END -->
SNIPPET

  # Remove any previous injection then re-inject before </head>.
  sed -i '/<!-- DEMO_AUTOLOGIN_START -->/,/<!-- DEMO_AUTOLOGIN_END -->/d' "$FRONTEND_INDEX"
  tmp_index="$(mktemp)"
  while IFS= read -r line; do
    if [[ "$line" == *"</head>"* ]]; then
      cat "$snippet" >> "$tmp_index"
    fi
    printf '%s\n' "$line" >> "$tmp_index"
  done < "$FRONTEND_INDEX"
  mv "$tmp_index" "$FRONTEND_INDEX"
  rm -f "$snippet"
}

echo "[demo] config:     $CONFIG_PATH"
echo "[demo] database:   $DB_PATH"
echo "[demo] wireguard:  $WG_DIR"
inject_demo_frontend_autologin
echo "[demo] autologin injected (read-only key)"

# ── Bootstrap: start server briefly so it initialises the DB schema ───────────
/app/ogs-swg -config "$CONFIG_PATH" --wg-test-mode &
BOOTSTRAP_PID=$!
trap 'kill "$BOOTSTRAP_PID" >/dev/null 2>&1 || true' EXIT INT TERM

/bin/bash /demo/fake-data-loop.sh bootstrap

kill "$BOOTSTRAP_PID" >/dev/null 2>&1 || true
wait "$BOOTSTRAP_PID" >/dev/null 2>&1 || true
trap - EXIT INT TERM

# ── Runtime loops ─────────────────────────────────────────────────────────────
# Log viewer: emit fake sing-box log lines to access.log.
/bin/bash /demo/fake-data-loop.sh logs &

# Dashboard: insert live traffic samples every DEMO_LIVE_INTERVAL_SEC.
/bin/bash /demo/fake-data-loop.sh sample &

# ── Main server ───────────────────────────────────────────────────────────────
exec /app/ogs-swg -config "$CONFIG_PATH" --wg-test-mode
