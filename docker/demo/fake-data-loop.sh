#!/usr/bin/env bash
set -euo pipefail

DB_PATH="${DEMO_DB_PATH:-/app/data/stats.db}"
LOG_PATH="${DEMO_LOG_PATH:-/app/data/access.log}"
MODE="${1:-sample}"
LIVE_INTERVAL="${DEMO_LIVE_INTERVAL_SEC:-60}"
LOG_INTERVAL="${DEMO_LOG_INTERVAL_SEC:-3}"
RETENTION="${DEMO_RETENTION_SECONDS:-172800}"

USERS=(user-01 user-02 user-03 user-04 user-05 user-06 user-07 user-08)
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

# Cumulative WG rx/tx tracked in memory throughout the process lifetime.
WG_RX=()
WG_TX=()

apply_sql() {
  local sql="$1"
  sqlite3 "$DB_PATH" <<SQL >/dev/null 2>&1 || true
PRAGMA busy_timeout = 5000;
BEGIN;
${sql}
COMMIT;
SQL
}

wait_for_db() {
  until sqlite3 "$DB_PATH" "SELECT 1 FROM samples LIMIT 1;" >/dev/null 2>&1; do
    sleep 1
  done
}

do_bootstrap() {
  local now i b ts hour base_mb per_user per_wg delta up down jitter ep sql inserted=0
  now=$(date +%s)

  # ── Users & peers ────────────────────────────────────────────────────────────
  sql=""
  for user in "${USERS[@]}"; do
    local quota=$(( (100 + RANDOM % 101) * 1024 * 1024 * 1024 ))
    sql+="INSERT INTO users(email,quota_limit,quota_period,reset_day,enabled) VALUES('${user}',${quota},'monthly',1,1) ON CONFLICT(email) DO UPDATE SET quota_limit=excluded.quota_limit,enabled=1;"
  done
  for i in "${!WG_KEYS[@]}"; do
    sql+="INSERT INTO wg_peers(public_key,alias,deleted,created_at,updated_at) VALUES('${WG_KEYS[$i]}','${WG_ALIASES[$i]}',0,${now},${now}) ON CONFLICT(public_key) DO UPDATE SET alias=excluded.alias,deleted=0,updated_at=${now};"
  done
  apply_sql "$sql"

  # ── Seed 48 half-hour buckets (24 h of history) ──────────────────────────────
  # Initialize cumulative WG counters with a random starting offset.
  for i in "${!WG_KEYS[@]}"; do
    WG_RX[$i]=$(( 20 * 1024 * 1024 + RANDOM % (80 * 1024 * 1024) ))
    WG_TX[$i]=$(( 12 * 1024 * 1024 + RANDOM % (60 * 1024 * 1024) ))
  done

  sql=""
  for ((b = 0; b < 48; b++)); do
    ts=$(( now - (48 - b) * 1800 ))
    hour=$(( (ts / 3600) % 24 ))

    # Diurnal traffic volume per bucket (MB)
    if   (( hour >= 0  && hour < 6  )); then base_mb=$(( 50  + RANDOM % 150 ))
    elif (( hour >= 6  && hour < 20 )); then base_mb=$(( 200 + RANDOM % 400 ))
    else                                     base_mb=$(( 150 + RANDOM % 250 ))
    fi

    # Sing-box: split evenly across users with small jitter.
    per_user=$(( base_mb * 1024 * 1024 / ${#USERS[@]} ))
    for user in "${USERS[@]}"; do
      jitter=$(( RANDOM % (per_user / 4 + 1) ))
      local total=$(( per_user + jitter ))
      up=$(( total * 35 / 100 ))
      down=$(( total - up ))
      sql+="INSERT OR REPLACE INTO samples(user,ts,uplink,downlink) VALUES('${user}',${ts},${up},${down});"
      inserted=$(( inserted + 1 ))
    done

    # WireGuard: cumulative rx/tx per peer.
    per_wg=$(( base_mb * 1024 * 1024 / ${#WG_KEYS[@]} ))
    for i in "${!WG_KEYS[@]}"; do
      delta=$(( per_wg + RANDOM % (per_wg / 4 + 1) ))
      WG_RX[$i]=$(( WG_RX[$i] + delta * 55 / 100 ))
      WG_TX[$i]=$(( WG_TX[$i] + delta * 45 / 100 ))
      ep=$(( 10 + i ))
      sql+="INSERT INTO wg_samples(public_key,ts,rx,tx,endpoint) VALUES('${WG_KEYS[$i]}',${ts},${WG_RX[$i]},${WG_TX[$i]},'198.51.100.${ep}:51820');"
      inserted=$(( inserted + 1 ))
    done

    # Flush every 8 buckets to keep transactions small.
    if (( (b + 1) % 8 == 0 )); then
      apply_sql "$sql"
      sql=""
    fi
  done
  [[ -n "$sql" ]] && apply_sql "$sql"

  apply_sql "INSERT INTO sampler_runs(ts,duration_ms,inserted,error,source) VALUES($(date +%s),500,${inserted},'','demo-seed');"
  echo "[demo-seeder] bootstrap: ${inserted} rows seeded"
}

do_sample_loop() {
  local i ts sql inserted loops=0 mb delta ep up down

  # Resume cumulative WG counters from the last values stored in the DB.
  for i in "${!WG_KEYS[@]}"; do
    WG_RX[$i]=$(sqlite3 "$DB_PATH" "SELECT COALESCE(MAX(rx),0) FROM wg_samples WHERE public_key='${WG_KEYS[$i]}';" 2>/dev/null || echo 0)
    WG_TX[$i]=$(sqlite3 "$DB_PATH" "SELECT COALESCE(MAX(tx),0) FROM wg_samples WHERE public_key='${WG_KEYS[$i]}';" 2>/dev/null || echo 0)
  done

  while true; do
    ts=$(date +%s)
    sql=""
    inserted=0

    for user in "${USERS[@]}"; do
      mb=$(( 1 + RANDOM % 8 ))
      local bytes=$(( mb * 512 * 1024 + RANDOM % (mb * 512 * 1024) ))
      up=$(( bytes * 35 / 100 ))
      down=$(( bytes - up ))
      sql+="INSERT OR REPLACE INTO samples(user,ts,uplink,downlink) VALUES('${user}',${ts},${up},${down});"
      inserted=$(( inserted + 1 ))
    done

    for i in "${!WG_KEYS[@]}"; do
      mb=$(( 1 + RANDOM % 8 ))
      delta=$(( mb * 512 * 1024 + RANDOM % (mb * 512 * 1024) ))
      WG_RX[$i]=$(( WG_RX[$i] + delta * 55 / 100 ))
      WG_TX[$i]=$(( WG_TX[$i] + delta * 45 / 100 ))
      ep=$(( 10 + i ))
      sql+="INSERT INTO wg_samples(public_key,ts,rx,tx,endpoint) VALUES('${WG_KEYS[$i]}',${ts},${WG_RX[$i]},${WG_TX[$i]},'198.51.100.${ep}:51820');"
      inserted=$(( inserted + 1 ))
    done

    sql+="INSERT INTO sampler_runs(ts,duration_ms,inserted,error,source) VALUES(${ts},50,${inserted},'','demo-live');"
    apply_sql "$sql"

    loops=$(( loops + 1 ))
    if (( loops % 30 == 0 )); then
      apply_sql "DELETE FROM samples WHERE ts < strftime('%s','now') - ${RETENTION};
                 DELETE FROM wg_samples WHERE ts < strftime('%s','now') - ${RETENTION};
                 DELETE FROM sampler_runs WHERE ts < strftime('%s','now') - ${RETENTION};"
      # Checkpoint the WAL so reads don't scan a bloated WAL file.
      sqlite3 "$DB_PATH" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1 || true
    fi

    sleep "$LIVE_INTERVAL"
  done
}

do_log_loop() {
  local -a CLIENTS=(198.51.100.210 203.0.113.212 192.0.2.214 198.51.100.211 192.0.2.215)
  local -a TARGETS=(cdn-edge.demo.invalid:443 api-gw.demo.invalid:443 media.demo.invalid:443 updates.demo.invalid:443 198.51.100.44:443)

  while true; do
    local sys_ts panel_ts tz inbound user conn latency port client target fsize
    # Rotate if > 1 MB so Log Viewer reads stay cheap.
    fsize=$(stat -c%s "$LOG_PATH" 2>/dev/null || echo 0)
    if (( fsize > 1048576 )); then
      : > "$LOG_PATH"
    fi

    sys_ts=$(date +"%b %d %H:%M:%S")
    panel_ts=$(date +"%Y-%m-%d %H:%M:%S")
    tz=$(date +"%z")
    inbound="${INBOUNDS[$((RANDOM % ${#INBOUNDS[@]}))]}"
    user="${USERS[$((RANDOM % ${#USERS[@]}))]}"
    conn=$(( 10000000 + RANDOM % 2000000000 ))
    latency=$(( 12 + RANDOM % 168 ))
    port=$(( 40000 + RANDOM % 25535 ))
    client="${CLIENTS[$((RANDOM % ${#CLIENTS[@]}))]}"
    target="${TARGETS[$((RANDOM % ${#TARGETS[@]}))]}"

    printf "%s localhost sing-box[2783211]: %s %s INFO [%s 0ms] inbound/vless[%s]: inbound connection from %s:%s\n" \
      "$sys_ts" "$tz" "$panel_ts" "$conn" "$inbound" "$client" "$port" >> "$LOG_PATH"
    printf "%s localhost sing-box[2783211]: %s %s INFO [%s %sms] inbound/vless[%s]: [%s] inbound connection to %s\n" \
      "$sys_ts" "$tz" "$panel_ts" "$conn" "$latency" "$inbound" "$user" "$target" >> "$LOG_PATH"

    sleep "$LOG_INTERVAL"
  done
}

case "$MODE" in
  bootstrap) wait_for_db; do_bootstrap ;;
  sample)    wait_for_db; do_sample_loop ;;
  logs)      do_log_loop ;;
  *) echo "[demo-seeder] unknown mode: $MODE" >&2; exit 1 ;;
esac
