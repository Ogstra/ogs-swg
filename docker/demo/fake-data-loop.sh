#!/usr/bin/env bash
set -euo pipefail

DB_PATH="${DEMO_DB_PATH:-/app/data/stats.db}"
LOG_PATH="${DEMO_LOG_PATH:-/app/data/access.log}"
MODE="${1:-live}"

if [[ "$MODE" != "logs" ]] && ! command -v sqlite3 >/dev/null 2>&1; then
  echo "[demo-seeder] sqlite3 is required in runtime image" >&2
  exit 1
fi

VLESS_USERS=(user-01 user-02 user-03 user-04 user-05 user-06 user-07 user-08)
INBOUNDS=(demo-vless-443 demo-vless-8443)
WG_KEYS=(
  "1VWtvDG+zP93WGArr7pqylpkqYjTDCy4ZrGfwHeXkLk="
  "bHhXePS0iXbBOjj0pHoNQXwPX8dch00de9gkw1/pQYE="
  "s1LzRtoFDZBrFmoHARjIbe3QRvTxiVN8NCvPyME20R0="
  "FkN+Ch9XRaWk+Fb0FyBobNEkKGU8OSaxNe1C9OmqzZE="
  "ZsEKnwCoSyPNlZJ0Oxe1+Z3qLPAQ+Ebs0VZOq/BYcyk="
  "Ek6fjFNQ87myJePoJtr+pm5hWWoBlvj/xXvVTGG2fq4="
  "IYAaBfH17FOodTqPaXKO0Vb2TFbUXld1AsaeK/Xmf8U="
  "kQjZRic2ApLtzdArsDCQ+1o0bbSHc9WheeTb0il0xb0="
  "YeTMKZ/DvjT/xHWPA4DZcu4j3paArQMfby9CCMT3MGI="
)
WG_ALIASES=(peer-01 peer-02 peer-03 peer-04 peer-05 peer-06 peer-07 peer-08 peer-09)

DEMO_MIN_TOTAL_GB="${DEMO_MIN_TOTAL_GB:-40}"
DEMO_MAX_TOTAL_GB="${DEMO_MAX_TOTAL_GB:-100}"
if (( DEMO_MIN_TOTAL_GB > DEMO_MAX_TOTAL_GB )); then
  DEMO_MAX_TOTAL_GB="$DEMO_MIN_TOTAL_GB"
fi
MIN_TOTAL_BYTES=$((DEMO_MIN_TOTAL_GB * 1024 * 1024 * 1024))
MAX_TOTAL_BYTES=$((DEMO_MAX_TOTAL_GB * 1024 * 1024 * 1024))
SEED_INTERVAL_SEC="${DEMO_SEED_INTERVAL_SEC:-300}" # 5 min
LIVE_INTERVAL_SEC="${DEMO_LIVE_INTERVAL_SEC:-60}"  # 1 min
LOG_INTERVAL_SEC="${DEMO_LOG_INTERVAL_SEC:-3}"     # 3 sec
RETENTION_SECONDS="${DEMO_RETENTION_SECONDS:-172800}"
if ! [[ "$RETENTION_SECONDS" =~ ^[0-9]+$ ]]; then
  echo "[demo-seeder] DEMO_RETENTION_SECONDS must be a positive integer, got: $RETENTION_SECONDS" >&2
  exit 1
fi
SEED_POINTS=$((24 * 3600 / SEED_INTERVAL_SEC))

WG_RX=()
WG_TX=()
SB_USER_BIAS_BPS=()
WG_PEER_BIAS_BPS=()
for _ in "${WG_KEYS[@]}"; do
  WG_RX+=("$((20 * 1024 * 1024 + RANDOM % (80 * 1024 * 1024)))")
  WG_TX+=("$((12 * 1024 * 1024 + RANDOM % (60 * 1024 * 1024)))")
done
for _ in "${VLESS_USERS[@]}"; do
  SB_USER_BIAS_BPS+=("$((220 + RANDOM % (3200 - 220 + 1)))")
done
for _ in "${WG_KEYS[@]}"; do
  WG_PEER_BIAS_BPS+=("$((200 + RANDOM % (3400 - 200 + 1)))")
done

build_active_set() {
  local total="$1"
  local target="$2"
  local step="$3"
  local start
  local k
  local idx
  local set=","

  if (( total <= 0 )); then
    echo ","
    return
  fi
  if (( target < 1 )); then
    target=1
  fi
  if (( target > total )); then
    target="$total"
  fi
  if (( step < 1 )); then
    step=1
  fi

  start=$((RANDOM % total))
  for ((k = 0; k < target; k++)); do
    idx=$(((start + k * step) % total))
    if [[ "$set" != *",$idx,"* ]]; then
      set+="$idx,"
      continue
    fi
    # Fallback when step/total are not coprime and we collide.
    local probe
    for ((probe = 0; probe < total; probe++)); do
      idx=$(((idx + 1) % total))
      if [[ "$set" != *",$idx,"* ]]; then
        set+="$idx,"
        break
      fi
    done
  done

  echo "$set"
}

is_set_member() {
  local set="$1"
  local idx="$2"
  [[ "$set" == *",$idx,"* ]]
}

SB_ACTIVE_TARGET="${DEMO_SB_ACTIVE_TARGET:-$((4 + RANDOM % 2))}"
WG_ACTIVE_TARGET="${DEMO_WG_ACTIVE_TARGET:-$((4 + RANDOM % 2))}"

# Keep active subsets stable for the full process lifetime so "last 5 min"
# never drifts above the configured cap.
SB_ACTIVE_SET="$(build_active_set "${#VLESS_USERS[@]}" "$SB_ACTIVE_TARGET" 3)"
WG_ACTIVE_SET="$(build_active_set "${#WG_KEYS[@]}" "$WG_ACTIVE_TARGET" 2)"

rand_between() {
  local min="$1"
  local max="$2"
  echo $((min + RANDOM % (max - min + 1)))
}

rand64_between() {
  local min="$1"
  local max="$2"
  local span=$((max - min + 1))
  local r
  # Compose a 45-bit pseudo-random integer from Bash RANDOM (15-bit each).
  r=$(((RANDOM << 30) | (RANDOM << 15) | RANDOM))
  echo $((min + (r % span)))
}

clamp() {
  local value="$1"
  local min="$2"
  local max="$3"
  if (( value < min )); then
    echo "$min"
    return
  fi
  if (( value > max )); then
    echo "$max"
    return
  fi
  echo "$value"
}

is_live_user_active() {
  local idx="$1"
  is_set_member "$SB_ACTIVE_SET" "$idx"
}

is_live_wg_peer_active() {
  local idx="$1"
  is_set_member "$WG_ACTIVE_SET" "$idx"
}

wait_for_db() {
  until [[ -f "$DB_PATH" ]] && sqlite3 "$DB_PATH" "SELECT 1 FROM sqlite_master WHERE type='table' AND name='samples';" | grep -q 1; do
    sleep 1
  done
}

apply_sql() {
  local sql="$1"
  sqlite3 "$DB_PATH" <<SQL >/dev/null 2>&1 || true
PRAGMA busy_timeout = 5000;
$sql
SQL
}

seed_reference_rows() {
  local sql="BEGIN;"
  local user
  for user in "${VLESS_USERS[@]}"; do
    local quota_gb quota_bytes
    quota_gb=$(rand_between 100 200)
    quota_bytes=$((quota_gb * 1024 * 1024 * 1024))
    sql+="INSERT INTO users(email, quota_limit, quota_period, reset_day, enabled) VALUES('${user}', ${quota_bytes}, 'monthly', 1, 1) ON CONFLICT(email) DO UPDATE SET quota_limit=excluded.quota_limit, quota_period=excluded.quota_period, reset_day=excluded.reset_day, enabled=1;"
  done
  local i
  for i in "${!WG_KEYS[@]}"; do
    sql+="INSERT INTO wg_peers(public_key, alias, deleted, created_at, updated_at) VALUES('${WG_KEYS[$i]}', '${WG_ALIASES[$i]}', 0, strftime('%s','now'), strftime('%s','now')) ON CONFLICT(public_key) DO UPDATE SET alias=excluded.alias, deleted=0, updated_at=strftime('%s','now');"
  done
  sql+="COMMIT;"
  apply_sql "$sql"
}

bucket_weight_bps() {
  local bucket_idx="$1"
  local hour=$(((bucket_idx * SEED_INTERVAL_SEC / 3600) % 24))
  local base
  case "$hour" in
    0|1|2|3|4|5) base=500 ;;
    6|7|8|9) base=900 ;;
    10|11|12|13|14|15|16) base=1200 ;;
    17|18|19|20|21|22) base=1850 ;;
    *) base=1000 ;;
  esac

  local jitter
  jitter=$(rand_between 320 2700)
  local weight=$((base * jitter / 1000))

  if (( RANDOM % 100 < 24 )); then
    weight=$((weight * $(rand_between 160 620) / 100))
  fi
  if (( RANDOM % 100 < 32 )); then
    weight=$((weight * $(rand_between 10 88) / 100))
  fi

  weight=$(clamp "$weight" 20 16000)
  echo "$weight"
}

append_bucket() {
  local ts="$1"
  local sb_total="$2"
  local wg_total="$3"
  local source="$4"

  local sql="BEGIN;"
  local inserted=0

  # sing-box: per-user randomness with occasional dominant users
  local sb_weights=()
  local sb_sum=0
  local sb_hot=-1
  if (( RANDOM % 100 < 58 )); then
    sb_hot=$((RANDOM % ${#VLESS_USERS[@]}))
  fi

  local i
  for i in "${!VLESS_USERS[@]}"; do
    local w
    if [[ "$source" == "demo-live" ]] && ! is_live_user_active "$i"; then
      w=0
    else
      w=$(rand_between 20 2600)
      w=$((w * SB_USER_BIAS_BPS[i] / 1000))
      if (( i == sb_hot )); then
        w=$((w * $(rand_between 240 780) / 100))
      fi
      if (( RANDOM % 100 < 22 )); then
        w=$((w * $(rand_between 8 42) / 100))
      fi
      if (( RANDOM % 100 < 18 )); then
        w=$((w * $(rand_between 180 520) / 100))
      fi
      w=$(clamp "$w" 1 250000)
    fi
    sb_weights+=("$w")
    sb_sum=$((sb_sum + w))
  done

  local sb_remaining="$sb_total"
  for i in "${!VLESS_USERS[@]}"; do
    local alloc
    if (( i == ${#VLESS_USERS[@]} - 1 )); then
      alloc="$sb_remaining"
    else
      alloc=$((sb_total * sb_weights[i] / sb_sum))
      if (( alloc > sb_remaining )); then
        alloc="$sb_remaining"
      fi
      sb_remaining=$((sb_remaining - alloc))
    fi

    local up_ratio up down
    up_ratio=$(rand_between 28 62)
    up=$((alloc * up_ratio / 100))
    down=$((alloc - up))
    sql+="INSERT OR REPLACE INTO samples(user, ts, uplink, downlink) VALUES('${VLESS_USERS[$i]}', ${ts}, ${up}, ${down});"
    inserted=$((inserted + 1))
  done

  # wireguard: per-peer randomness with non-linear deltas
  local wg_weights=()
  local wg_sum=0
  local wg_hot=-1
  if (( RANDOM % 100 < 54 )); then
    wg_hot=$((RANDOM % ${#WG_KEYS[@]}))
  fi

  for i in "${!WG_KEYS[@]}"; do
    local w
    if [[ "$source" == "demo-live" ]] && ! is_live_wg_peer_active "$i"; then
      w=0
    else
      w=$(rand_between 15 2500)
      w=$((w * WG_PEER_BIAS_BPS[i] / 1000))
      if (( i == wg_hot )); then
        w=$((w * $(rand_between 230 760) / 100))
      fi
      if (( RANDOM % 100 < 24 )); then
        w=$((w * $(rand_between 6 40) / 100))
      fi
      if (( RANDOM % 100 < 16 )); then
        w=$((w * $(rand_between 180 520) / 100))
      fi
      w=$(clamp "$w" 1 250000)
    fi
    wg_weights+=("$w")
    wg_sum=$((wg_sum + w))
  done

  local wg_remaining="$wg_total"
  for i in "${!WG_KEYS[@]}"; do
    local alloc
    if (( i == ${#WG_KEYS[@]} - 1 )); then
      alloc="$wg_remaining"
    else
      alloc=$((wg_total * wg_weights[i] / wg_sum))
      if (( alloc > wg_remaining )); then
        alloc="$wg_remaining"
      fi
      wg_remaining=$((wg_remaining - alloc))
    fi

    local rx_ratio rx_inc tx_inc endpoint_octet
    rx_ratio=$(rand_between 52 84)
    rx_inc=$((alloc * rx_ratio / 100))
    tx_inc=$((alloc - rx_inc))

    WG_RX[$i]=$((WG_RX[$i] + rx_inc))
    WG_TX[$i]=$((WG_TX[$i] + tx_inc))

    endpoint_octet=$((10 + i))
    sql+="INSERT INTO wg_samples(public_key, ts, rx, tx, endpoint) VALUES('${WG_KEYS[$i]}', ${ts}, ${WG_RX[$i]}, ${WG_TX[$i]}, '198.51.100.${endpoint_octet}:51820');"
    inserted=$((inserted + 1))
  done

  sql+="INSERT INTO sampler_runs(ts, duration_ms, inserted, error, source) VALUES(${ts}, $((15 + RANDOM % 260)), ${inserted}, '', '${source}');"
  sql+="COMMIT;"
  apply_sql "$sql"
}

seed_bootstrap_history() {
  local target_total target_sb target_wg sb_share_bps
  target_total=$(rand64_between "$MIN_TOTAL_BYTES" "$MAX_TOTAL_BYTES")
  sb_share_bps=$(rand_between 460 710)
  target_sb=$((target_total * sb_share_bps / 1000))
  target_wg=$((target_total - target_sb))

  echo "[demo-seeder] target last24h total=$(awk "BEGIN {printf \"%.2f\", $target_total/1024/1024/1024}") GiB (sb=$(awk "BEGIN {printf \"%.1f\", $sb_share_bps/10}")%)"

  local weights=()
  local weight_sum=0
  local i
  for ((i = 0; i < SEED_POINTS; i++)); do
    local w
    w=$(bucket_weight_bps "$i")
    weights+=("$w")
    weight_sum=$((weight_sum + w))
  done

  local now boot_start
  now=$(date +%s)
  # End seed data a bit before "now" so the initial "active in last 5m"
  # comes from live loop behavior, not from the last seed bucket.
  boot_start=$((now - 24 * 3600 - SEED_INTERVAL_SEC))

  local sb_remaining="$target_sb"
  local wg_remaining="$target_wg"

  for ((i = 0; i < SEED_POINTS; i++)); do
    local ts sb_bucket wg_bucket
    ts=$((boot_start + i * SEED_INTERVAL_SEC))

    if (( i == SEED_POINTS - 1 )); then
      sb_bucket="$sb_remaining"
      wg_bucket="$wg_remaining"
    else
      sb_bucket=$((target_sb * weights[i] / weight_sum))
      wg_bucket=$((target_wg * weights[i] / weight_sum))
      if (( sb_bucket > sb_remaining )); then sb_bucket="$sb_remaining"; fi
      if (( wg_bucket > wg_remaining )); then wg_bucket="$wg_remaining"; fi
      sb_remaining=$((sb_remaining - sb_bucket))
      wg_remaining=$((wg_remaining - wg_bucket))
    fi

    append_bucket "$ts" "$sb_bucket" "$wg_bucket" "demo-seed"
  done
}

emit_log_line() {
  local sys_ts panel_ts tz_offset inbound conn_id latency_ms client_port client_ip target_host user_name
  local -a client_ips=(
    "198.51.100.210"
    "198.51.100.211"
    "203.0.113.212"
    "203.0.113.213"
    "192.0.2.214"
    "192.0.2.215"
  )
  local -a targets=(
    "cdn-edge-01.demo.invalid:443"
    "api-gateway.demo.invalid:8443"
    "media-cache.demo.invalid:443"
    "updates.demo.invalid:443"
    "auth-node.demo.invalid:443"
    "198.51.100.44:443"
    "203.0.113.77:8443"
    "192.0.2.25:443"
  )

  sys_ts=$(date +"%b %d %H:%M:%S")
  panel_ts=$(date +"%Y-%m-%d %H:%M:%S")
  tz_offset=$(date +"%z")
  inbound="${INBOUNDS[$((RANDOM % ${#INBOUNDS[@]}))]}"
  if [[ "$inbound" == "demo-vless-443" ]]; then
    user_name="${VLESS_USERS[$((RANDOM % 4))]}"
  else
    user_name="${VLESS_USERS[$((4 + RANDOM % 4))]}"
  fi
  conn_id=$(rand_between 10000000 2147483000)
  latency_ms=$(rand_between 12 180)
  client_port=$(rand_between 40000 65535)
  client_ip="${client_ips[$((RANDOM % ${#client_ips[@]}))]}"
  target_host="${targets[$((RANDOM % ${#targets[@]}))]}"

  printf "%s localhost sing-box[2783211]: %s %s INFO [%s 0ms] inbound/vless[%s]: inbound connection from %s:%s\n" \
    "$sys_ts" "$tz_offset" "$panel_ts" "$conn_id" "$inbound" "$client_ip" "$client_port" >> "$LOG_PATH"

  printf "%s localhost sing-box[2783211]: %s %s INFO [%s %sms] inbound/vless[%s]: [%s] inbound connection to %s\n" \
    "$sys_ts" "$tz_offset" "$panel_ts" "$conn_id" "$latency_ms" "$inbound" "$user_name" "$target_host" >> "$LOG_PATH"

  printf "%s localhost sing-box[2783211]: %s %s INFO [%s %sms] outbound/direct[direct]: outbound connection to %s\n" \
    "$sys_ts" "$tz_offset" "$panel_ts" "$conn_id" "$latency_ms" "$target_host" >> "$LOG_PATH"
}

log_loop() {
  while true; do
    emit_log_line
    sleep "$LOG_INTERVAL_SEC"
  done
}

sample_loop() {
  local target_total target_per_minute live_bps loops
  target_total=$(rand64_between "$MIN_TOTAL_BYTES" "$MAX_TOTAL_BYTES")
  target_per_minute=$((target_total / 1440))
  live_bps=$(rand_between 620 1520)
  loops=0

  while true; do
    local delta jump sb_share sb_total wg_total ts

    delta=$((RANDOM % 1041 - 520))
    live_bps=$((live_bps + delta))
    live_bps=$(clamp "$live_bps" 160 4200)

    if (( RANDOM % 100 < 24 )); then
      jump=$(rand_between 170 420)
      live_bps=$((live_bps * jump / 100))
      live_bps=$(clamp "$live_bps" 160 4800)
    fi
    if (( RANDOM % 100 < 28 )); then
      jump=$(rand_between 14 78)
      live_bps=$((live_bps * jump / 100))
      live_bps=$(clamp "$live_bps" 110 4800)
    fi
    if (( RANDOM % 100 < 7 )); then
      live_bps=$((live_bps * $(rand_between 250 600) / 100))
      live_bps=$(clamp "$live_bps" 110 6500)
    fi
    if (( RANDOM % 100 < 9 )); then
      live_bps=$((live_bps * $(rand_between 8 44) / 100))
      live_bps=$(clamp "$live_bps" 70 6500)
    fi

    local total_this_min
    total_this_min=$((target_per_minute * live_bps / 1000))
    total_this_min=$(clamp "$total_this_min" $((target_per_minute / 12)) $((target_per_minute * 12)))

    sb_share=$(rand_between 430 760)
    sb_total=$((total_this_min * sb_share / 1000))
    wg_total=$((total_this_min - sb_total))

    ts=$(date +%s)
    append_bucket "$ts" "$sb_total" "$wg_total" "demo-live"

    loops=$((loops + 1))
    if (( loops % 30 == 0 )); then
      apply_sql "BEGIN;DELETE FROM samples WHERE ts < strftime('%s','now') - ${RETENTION_SECONDS};DELETE FROM wg_samples WHERE ts < strftime('%s','now') - ${RETENTION_SECONDS};DELETE FROM sampler_runs WHERE ts < strftime('%s','now') - ${RETENTION_SECONDS};COMMIT;"
    fi

    sleep "$LIVE_INTERVAL_SEC"
  done
}

if [[ "$MODE" == "logs" ]]; then
  log_loop
  exit 0
fi

if [[ "$MODE" == "sample" ]]; then
  wait_for_db
  sample_loop
  exit 0
fi

wait_for_db
seed_reference_rows
seed_bootstrap_history

if [[ "$MODE" == "bootstrap" ]]; then
  exit 0
fi

log_loop &
LOG_PID=$!
trap 'kill "$LOG_PID" >/dev/null 2>&1 || true' EXIT INT TERM
sample_loop
