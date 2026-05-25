#!/usr/bin/env bash
set -euo pipefail

DB_PATH="${DEMO_DB_PATH:-/app/data/stats.db}"
LOG_PATH="${DEMO_LOG_PATH:-/app/data/access.log}"
LOG_DB_PATH="${DEMO_LOG_DB_PATH:-$(dirname "$DB_PATH")/singbox_logs.db}"
MODE="${1:-sample}"
LIVE_INTERVAL="${DEMO_LIVE_INTERVAL_SEC:-60}"
LOG_INTERVAL="${DEMO_LOG_INTERVAL_SEC:-3}"
SUB_REQUEST_INTERVAL="${DEMO_SUB_REQUEST_INTERVAL_SEC:-8}"
RETENTION="${DEMO_RETENTION_SECONDS:-172800}"

USERS=(user-01 user-02 user-03 user-04 user-05 user-06 user-07 user-08 user-09 user-10)
INBOUNDS=(demo-vless-reality demo-vmess-ws demo-trojan-tls demo-hysteria2 demo-shadowsocks-2022 demo-anytls demo-naive)
INBOUND_PROTOCOLS=(vless vmess trojan hysteria2 shadowsocks anytls naive)

SUB_TOKENS=(demo-sub-mobile demo-sub-overquota demo-sub-cached demo-sub-unlimited demo-sub-modern)
SUB_NAMES=("Reality Mobile" "Mixed Office Over Quota" "Cached VMess/Trojan" "Unlimited UDP Lab" "AnyTLS and Naive Lab")
SUB_QUOTAS=($((60 * 1024 * 1024 * 1024)) $((3 * 1024 * 1024 * 1024)) $((24 * 1024 * 1024 * 1024)) 0 $((18 * 1024 * 1024 * 1024)))
SUB_INTERVALS=(24 6 12 0 8)
SUB_UPDATE_ALWAYS=(0 1 0 1 0)
SUB_USERS=("user-01,user-02" "user-03,user-05" "user-04,user-06" "user-07,user-08" "user-09,user-10")

REQUEST_UAS=(
  "Shadowrocket/2.2.82 CFNetwork/1568.200.51 Darwin/24.1.0"
  "Stash/2.6.1 (iPhone16,2; iOS 18.3.1)"
  "v2rayN/Windows/7.12"
  "Nekoray/Linux/4.1.0"
  "sing-box/1.12.0"
  "ClashMetaForAndroid/2.11.7"
)
REQUEST_MODELS=("iPhone16,2" "MacBookPro18,3" "PC" "Pixel 9 Pro" "SM-S928B" "iPad14,5")
REQUEST_OS=("iOS" "macOS" "Windows" "Android" "Android" "iPadOS")
REQUEST_OS_VERSIONS=("18.3.1" "15.3.1" "11" "15" "14" "18.2")
REQUEST_APPS=("2.2.82" "1.0.0" "7.12" "4.1.0" "1.12.0" "2.11.7")
REQUEST_COUNTRIES=("AR" "CL" "UY" "BR" "US" "DE")
REQUEST_IPS=("198.51.100.110" "2001:db8:85a3::8a2e:370:7334" "198.51.100.112" "203.0.113.114" "2001:db8:1:2::115" "2001:db8:ffff:1::116")

BLOCKED_UAS=(
  "Mozilla/5.0 (Macintosh; Intel Mac OS X 15_3_1) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.3 Safari/605.1.15"
  "Slackbot-LinkExpanding 1.0 (+https://api.slack.com/robots)"
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"
)
BLOCKED_REASONS=("ip_block" "token_block" "rate_limit" "ua_social_fetcher" "ua_browser")

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

join_users_for_sub() {
  local idx="$1"
  printf '%s' "${SUB_USERS[$idx]}" | sed 's/,/, /g'
}

sub_id_sql() {
  local idx="$1"
  printf "(SELECT id FROM subscriptions WHERE token='%s')" "${SUB_TOKENS[$idx]}"
}

subscription_alias_for_user() {
  case "$1" in
    user-01) printf 'Reality Phone' ;;
    user-02) printf 'Reality Laptop' ;;
    user-03) printf 'VMess Desktop' ;;
    user-04) printf 'VMess Tablet' ;;
    user-05) printf 'Trojan Office' ;;
    user-06) printf 'Hysteria2 Travel' ;;
    user-07) printf 'Shadowsocks TCP' ;;
    user-08) printf 'Shadowsocks UDP' ;;
    user-09) printf 'AnyTLS Laptop' ;;
    user-10) printf 'Naive Browser' ;;
    *) printf '%s' "$1" ;;
  esac
}

wait_for_db() {
  until sqlite3 "$DB_PATH" "SELECT 1 FROM samples LIMIT 1;" >/dev/null 2>&1; do
    sleep 1
  done
}

wait_for_log_db() {
  until sqlite3 "$LOG_DB_PATH" "SELECT 1 FROM singbox_logs LIMIT 1;" >/dev/null 2>&1; do
    sleep 1
  done
}

sql_quote() {
  printf "%s" "$1" | sed "s/'/''/g"
}

insert_demo_log_lines() {
  local ts_ms="$1"
  local first_line="$2"
  local second_line="$3"
  local user="$4"
  local first_quoted second_quoted user_quoted

  first_quoted="$(sql_quote "$first_line")"
  second_quoted="$(sql_quote "$second_line")"
  user_quoted="$(sql_quote "[$user]")"

  sqlite3 "$LOG_DB_PATH" <<SQL >/dev/null 2>&1 || true
PRAGMA busy_timeout = 5000;
BEGIN;
INSERT INTO singbox_logs(ts,raw,level,user) VALUES(${ts_ms},'${first_quoted}','INFO','');
INSERT INTO singbox_logs_fts(rowid, raw) VALUES(last_insert_rowid(),'${first_quoted}');
INSERT INTO singbox_logs(ts,raw,level,user) VALUES(${ts_ms},'${second_quoted}','INFO','${user_quoted}');
INSERT INTO singbox_logs_fts(rowid, raw) VALUES(last_insert_rowid(),'${second_quoted}');
COMMIT;
SQL
}

seed_core_entities() {
  local now="$1"
  local sql="" i user interval_sql credential flow vmess_security inbound_tags

  for i in "${!USERS[@]}"; do
    user="${USERS[$i]}"
    local quota=$(( (100 + RANDOM % 101) * 1024 * 1024 * 1024 ))
    credential=""
    flow=""
    vmess_security=""
    inbound_tags="[]"
    case "$user" in
      user-01)
        credential="11111111-1111-4111-8111-111111111111"
        flow="xtls-rprx-vision"
        inbound_tags='["demo-vless-reality"]'
        ;;
      user-02)
        credential="22222222-2222-4222-8222-222222222222"
        flow="xtls-rprx-vision"
        inbound_tags='["demo-vless-reality"]'
        ;;
      user-03)
        credential="33333333-3333-4333-8333-333333333333"
        vmess_security="auto"
        inbound_tags='["demo-vmess-ws"]'
        ;;
      user-04)
        credential="44444444-4444-4444-8444-444444444444"
        vmess_security="auto"
        inbound_tags='["demo-vmess-ws"]'
        ;;
      user-05)
        credential="demo-trojan-user-05-pass"
        inbound_tags='["demo-trojan-tls"]'
        ;;
      user-06)
        credential="demo-hysteria2-user-06-pass"
        inbound_tags='["demo-hysteria2"]'
        ;;
      user-07)
        credential="YWJjZGVmMDEyMzQ1Njc4OQ=="
        inbound_tags='["demo-shadowsocks-2022"]'
        ;;
      user-08)
        credential="ZmVkY2JhOTg3NjU0MzIxMA=="
        inbound_tags='["demo-shadowsocks-2022"]'
        ;;
      user-09)
        credential="demo-anytls-user-09-pass"
        inbound_tags='["demo-anytls"]'
        ;;
      user-10)
        credential="demo-naive-user-10-pass"
        inbound_tags='["demo-naive"]'
        ;;
    esac
    sql+="INSERT INTO users(email,quota_limit,quota_period,reset_day,enabled,credential,flow,vmess_security,inbound_tags) VALUES('${user}',${quota},'monthly',1,1,'${credential}','${flow}','${vmess_security}','${inbound_tags}') ON CONFLICT(email) DO UPDATE SET quota_limit=excluded.quota_limit,enabled=1,credential=excluded.credential,flow=excluded.flow,vmess_security=excluded.vmess_security,inbound_tags=excluded.inbound_tags;"
  done

  for i in "${!WG_KEYS[@]}"; do
    sql+="INSERT INTO wg_peers(public_key,alias,deleted,created_at,updated_at) VALUES('${WG_KEYS[$i]}','${WG_ALIASES[$i]}',0,${now},${now}) ON CONFLICT(public_key) DO UPDATE SET alias=excluded.alias,deleted=0,updated_at=${now};"
  done

  for i in "${!SUB_TOKENS[@]}"; do
    interval_sql="NULL"
    if (( ${SUB_INTERVALS[$i]} > 0 )); then
      interval_sql="${SUB_INTERVALS[$i]}"
    fi
    sql+="INSERT INTO subscriptions(token,name,quota_limit,quota_period,reset_day,profile_update_interval_hours,update_always,created_at,updated_at) VALUES('${SUB_TOKENS[$i]}','${SUB_NAMES[$i]}',${SUB_QUOTAS[$i]},'monthly',1,${interval_sql},${SUB_UPDATE_ALWAYS[$i]},${now},${now}) ON CONFLICT(token) DO UPDATE SET name=excluded.name,quota_limit=excluded.quota_limit,quota_period='monthly',reset_day=1,profile_update_interval_hours=excluded.profile_update_interval_hours,update_always=excluded.update_always,updated_at=${now};"
  done

  apply_sql "$sql"

  sql=""
  for i in "${!SUB_TOKENS[@]}"; do
    local -a sub_users=()
    IFS=',' read -r -a sub_users <<< "${SUB_USERS[$i]}"
    for user in "${sub_users[@]}"; do
      sql+="INSERT OR REPLACE INTO subscription_users(sub_id,user_name,alias) VALUES($(sub_id_sql "$i"),'${user}','$(subscription_alias_for_user "$user")');"
    done
  done
  sql+="INSERT INTO subscription_protection_rules(rule_type,value,note,created_at) VALUES"
  sql+="('ip_block','2001:db8:bad:cafe::199','Known abusive preview fetcher',${now}),"
  sql+="('token_block','demo-sub-overquota','Temporarily block compromised token',${now}),"
  sql+="('ip_allow','2001:db8:ffff:1::116','Trusted office egress',${now});"
  apply_sql "$sql"
}

seed_traffic_history() {
  local now="$1"
  local i b ts hour base_mb per_user per_wg delta up down jitter ep sql="" inserted=0

  for i in "${!WG_KEYS[@]}"; do
    WG_RX[$i]=$(( 20 * 1024 * 1024 + RANDOM % (80 * 1024 * 1024) ))
    WG_TX[$i]=$(( 12 * 1024 * 1024 + RANDOM % (60 * 1024 * 1024) ))
  done

  for ((b = 0; b < 48; b++)); do
    ts=$(( now - (48 - b) * 1800 ))
    hour=$(( (ts / 3600) % 24 ))

    if   (( hour >= 0  && hour < 6  )); then base_mb=$(( 50  + RANDOM % 150 ))
    elif (( hour >= 6  && hour < 20 )); then base_mb=$(( 200 + RANDOM % 400 ))
    else                                     base_mb=$(( 150 + RANDOM % 250 ))
    fi

    per_user=$(( base_mb * 1024 * 1024 / ${#USERS[@]} ))
    for user in "${USERS[@]}"; do
      jitter=$(( RANDOM % (per_user / 4 + 1) ))
      local total=$(( per_user + jitter ))
      up=$(( total * 35 / 100 ))
      down=$(( total - up ))
      sql+="INSERT OR REPLACE INTO samples(user,ts,uplink,downlink) VALUES('${user}',${ts},${up},${down});"
      inserted=$(( inserted + 1 ))
    done

    per_wg=$(( base_mb * 1024 * 1024 / ${#WG_KEYS[@]} ))
    for i in "${!WG_KEYS[@]}"; do
      delta=$(( per_wg + RANDOM % (per_wg / 4 + 1) ))
      WG_RX[$i]=$(( WG_RX[$i] + delta * 55 / 100 ))
      WG_TX[$i]=$(( WG_TX[$i] + delta * 45 / 100 ))
      ep=$(( 10 + i ))
      sql+="INSERT INTO wg_samples(public_key,ts,rx,tx,endpoint) VALUES('${WG_KEYS[$i]}',${ts},${WG_RX[$i]},${WG_TX[$i]},'198.51.100.${ep}:51820');"
      inserted=$(( inserted + 1 ))
    done

    if (( (b + 1) % 8 == 0 )); then
      apply_sql "$sql"
      sql=""
    fi
  done

  [[ -n "$sql" ]] && apply_sql "$sql"
  printf '%s' "$inserted"
}

seed_subscription_history() {
  local now="$1"
  local sql="" i sub_idx ua_idx ip_idx req_ts served_from_cache blocked reason req_ua request_count=0

  for ((i = 0; i < 18; i++)); do
    sub_idx=$(( i % 4 ))
    ua_idx=$(( i % ${#REQUEST_UAS[@]} ))
    ip_idx=$(( i % ${#REQUEST_IPS[@]} ))
    req_ts=$(( now - (18 - i) * 480 ))
    served_from_cache=0
    blocked=0
    reason=""
    req_ua="${REQUEST_UAS[$ua_idx]}"

    if (( i % 6 == 2 || i % 6 == 5 )); then
      served_from_cache=1
    fi

    case "$i" in
      4)
        blocked=1
        served_from_cache=0
        reason="ip_block"
        req_ua="${BLOCKED_UAS[0]}"
        ip_idx=0
        ;;
      9)
        blocked=1
        served_from_cache=0
        reason="token_block"
        req_ua="${REQUEST_UAS[2]}"
        sub_idx=1
        ;;
      13)
        blocked=1
        served_from_cache=0
        reason="ua_social_fetcher"
        req_ua="${BLOCKED_UAS[1]}"
        sub_idx=2
        ;;
      16)
        blocked=1
        served_from_cache=0
        reason="rate_limit"
        req_ua="${REQUEST_UAS[4]}"
        sub_idx=0
        ;;
    esac

    sql+="INSERT INTO subscription_requests(sub_id,user_name,request_ip,request_host,request_path,user_agent,device_model,device_os,device_os_version,app_version,country,hwid_hash,hwid_prefix,requested_at,served_from_cache,blocked,block_reason) VALUES("
    sql+="$(sub_id_sql "$sub_idx"),"
    sql+="'$(join_users_for_sub "$sub_idx")',"
    sql+="'${REQUEST_IPS[$ip_idx]}',"
    sql+="'swg-demo.local',"
    sql+="'/s/${SUB_TOKENS[$sub_idx]}',"
    sql+="'${req_ua}',"
    sql+="'${REQUEST_MODELS[$ua_idx]}',"
    sql+="'${REQUEST_OS[$ua_idx]}',"
    sql+="'${REQUEST_OS_VERSIONS[$ua_idx]}',"
    sql+="'${REQUEST_APPS[$ua_idx]}',"
    sql+="'${REQUEST_COUNTRIES[$ua_idx]}',"
    sql+="'demo-hash-$((1000 + i))',"
    sql+="'D$((100 + i))',"
    sql+="${req_ts},${served_from_cache},${blocked},'${reason}');"
    request_count=$(( request_count + 1 ))
  done

  apply_sql "$sql"
  printf '%s' "$request_count"
}

do_bootstrap() {
  local now inserted_traffic inserted_requests inserted_total
  now=$(date +%s)

  seed_core_entities "$now"
  inserted_traffic=$(seed_traffic_history "$now")
  inserted_requests=$(seed_subscription_history "$now")
  inserted_total=$(( inserted_traffic + inserted_requests ))

  apply_sql "INSERT INTO sampler_runs(ts,duration_ms,inserted,error,source) VALUES(${now},500,${inserted_total},'','demo-seed');"
  echo "[demo-seeder] bootstrap: ${inserted_total} rows seeded"
}

do_sample_loop() {
  local i ts sql inserted loops=0 mb delta ep up down

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
      sqlite3 "$DB_PATH" "PRAGMA wal_checkpoint(TRUNCATE);" >/dev/null 2>&1 || true
    fi

    sleep "$LIVE_INTERVAL"
  done
}

do_log_loop() {
  local -a CLIENTS=(198.51.100.210 2001:db8:feed:beef::212 192.0.2.214 2001:db8:abcd:12::211 192.0.2.215)
  local -a TARGETS=(cdn-edge.demo.invalid:443 api-gw.demo.invalid:443 media.demo.invalid:443 updates.demo.invalid:443 198.51.100.44:443)

  wait_for_log_db
  while true; do
    local sys_ts panel_ts ts_ms tz inbound_idx inbound protocol user conn latency port client target fsize line1 line2
    fsize=$(stat -c%s "$LOG_PATH" 2>/dev/null || echo 0)
    if (( fsize > 1048576 )); then
      : > "$LOG_PATH"
    fi

    sys_ts=$(date +"%b %d %H:%M:%S")
    panel_ts=$(date +"%Y-%m-%d %H:%M:%S")
    ts_ms=$(( $(date +%s) * 1000 ))
    tz=$(date +"%z")
    inbound_idx=$((RANDOM % ${#INBOUNDS[@]}))
    inbound="${INBOUNDS[$inbound_idx]}"
    protocol="${INBOUND_PROTOCOLS[$inbound_idx]}"
    user="${USERS[$((RANDOM % ${#USERS[@]}))]}"
    conn=$(( 10000000 + RANDOM % 2000000000 ))
    latency=$(( 12 + RANDOM % 168 ))
    port=$(( 40000 + RANDOM % 25535 ))
    client="${CLIENTS[$((RANDOM % ${#CLIENTS[@]}))]}"
    target="${TARGETS[$((RANDOM % ${#TARGETS[@]}))]}"

    line1=$(printf "%s localhost sing-box[2783211]: %s %s INFO [%s 0ms] inbound/%s[%s]: inbound connection from %s:%s" \
      "$sys_ts" "$tz" "$panel_ts" "$conn" "$protocol" "$inbound" "$client" "$port")
    line2=$(printf "%s localhost sing-box[2783211]: %s %s INFO [%s %sms] inbound/%s[%s]: [%s] inbound connection to %s" \
      "$sys_ts" "$tz" "$panel_ts" "$conn" "$latency" "$protocol" "$inbound" "$user" "$target")

    printf "%s\n%s\n" "$line1" "$line2" >> "$LOG_PATH"
    insert_demo_log_lines "$ts_ms" "$line1" "$line2" "$user"

    sleep "$LOG_INTERVAL"
  done
}

do_subscription_loop() {
  local ts sql loops=0 sub_idx ua_idx ip_idx mode served_from_cache blocked reason req_ua

  while true; do
    ts=$(date +%s)
    sub_idx=$(( RANDOM % ${#SUB_TOKENS[@]} ))
    ua_idx=$(( RANDOM % ${#REQUEST_UAS[@]} ))
    ip_idx=$(( RANDOM % ${#REQUEST_IPS[@]} ))
    mode=$(( RANDOM % 10 ))
    served_from_cache=0
    blocked=0
    reason=""
    req_ua="${REQUEST_UAS[$ua_idx]}"

    if (( mode >= 7 && mode <= 8 )); then
      served_from_cache=1
    fi

    if (( mode == 9 )); then
      blocked=1
      served_from_cache=0
      reason="${BLOCKED_REASONS[$(( RANDOM % ${#BLOCKED_REASONS[@]} ))]}"
      req_ua="${BLOCKED_UAS[$(( RANDOM % ${#BLOCKED_UAS[@]} ))]}"
      if [[ "$reason" == "token_block" ]]; then
        sub_idx=1
      fi
    fi

    sql="INSERT INTO subscription_requests(sub_id,user_name,request_ip,request_host,request_path,user_agent,device_model,device_os,device_os_version,app_version,country,hwid_hash,hwid_prefix,requested_at,served_from_cache,blocked,block_reason) VALUES("
    sql+="$(sub_id_sql "$sub_idx"),"
    sql+="'$(join_users_for_sub "$sub_idx")',"
    sql+="'${REQUEST_IPS[$ip_idx]}',"
    sql+="'swg-demo.local',"
    sql+="'/s/${SUB_TOKENS[$sub_idx]}',"
    sql+="'${req_ua}',"
    sql+="'${REQUEST_MODELS[$ua_idx]}',"
    sql+="'${REQUEST_OS[$ua_idx]}',"
    sql+="'${REQUEST_OS_VERSIONS[$ua_idx]}',"
    sql+="'${REQUEST_APPS[$ua_idx]}',"
    sql+="'${REQUEST_COUNTRIES[$ua_idx]}',"
    sql+="'demo-live-$((ts % 100000))',"
    sql+="'L$((ts % 1000))',"
    sql+="${ts},${served_from_cache},${blocked},'${reason}');"
    apply_sql "$sql"

    loops=$(( loops + 1 ))
    if (( loops % 30 == 0 )); then
      apply_sql "DELETE FROM subscription_requests WHERE requested_at < strftime('%s','now') - ${RETENTION};"
    fi

    sleep "$SUB_REQUEST_INTERVAL"
  done
}

case "$MODE" in
  bootstrap)     wait_for_db; do_bootstrap ;;
  sample)        wait_for_db; do_sample_loop ;;
  logs)          do_log_loop ;;
  subscriptions) wait_for_db; do_subscription_loop ;;
  *) echo "[demo-seeder] unknown mode: $MODE" >&2; exit 1 ;;
esac
