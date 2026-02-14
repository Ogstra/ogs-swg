#!/usr/bin/env bash
set -euo pipefail

DEPLOY_ROOT="${DEPLOY_ROOT:-/state}"
ACTIVE_FILE="${DEPLOY_ROOT}/${ACTIVE_FILE:-.bluegreen.active}"
PREVIOUS_FILE="${DEPLOY_ROOT}/${PREVIOUS_FILE:-.bluegreen.previous}"
BAKE_UNTIL_FILE="${DEPLOY_ROOT}/${BAKE_UNTIL_FILE:-.bluegreen.bake_until}"
EVENTS_FILE="${DEPLOY_ROOT}/${EVENTS_FILE:-.bluegreen.events.log}"
NGINX_TEMPLATE="${DEPLOY_ROOT}/${NGINX_TEMPLATE:-docker/bluegreen/nginx/default.conf.template}"
NGINX_CONF="${DEPLOY_ROOT}/${NGINX_CONF:-docker/bluegreen/nginx/default.conf}"
HEALTH_URL_PATH="${HEALTH_URL_PATH:-/api/login}"
HEALTH_METHOD="${HEALTH_METHOD:-POST}"
HEALTH_BODY="${HEALTH_BODY:-{\"username\":\"invalid\",\"password\":\"invalid\"}}"
HEALTH_EXPECT="${HEALTH_EXPECT:-401,405}"
CHECK_INTERVAL_SEC="${CHECK_INTERVAL_SEC:-15}"
FAIL_THRESHOLD="${FAIL_THRESHOLD:-4}"

log_event() {
  local msg="$1"
  printf '%s %s\n' "$(date -u +'%Y-%m-%dT%H:%M:%SZ')" "$msg" | tee -a "$EVENTS_FILE" >/dev/null
}

slot_container() {
  printf 'ogs-swg-%s' "$1"
}

slot_exists() {
  docker container inspect "$(slot_container "$1")" >/dev/null 2>&1
}

slot_running() {
  [ "$(docker inspect -f '{{.State.Running}}' "$(slot_container "$1")" 2>/dev/null || echo false)" = "true" ]
}

slot_health_ok() {
  local slot="$1"
  local code
  if ! slot_running "$slot"; then
    return 1
  fi

  code="$(curl -s -o /dev/null -w '%{http_code}' \
    -X "$HEALTH_METHOD" \
    -H 'Content-Type: application/json' \
    -d "$HEALTH_BODY" \
    "http://$(slot_container "$slot"):8080${HEALTH_URL_PATH}" || true)"

  IFS=',' read -r -a expected_codes <<< "$HEALTH_EXPECT"
  for expected in "${expected_codes[@]}"; do
    if [ "$code" = "$expected" ]; then
      return 0
    fi
  done
  return 1
}

switch_proxy_to() {
  local slot="$1"
  sed "s/__UPSTREAM__/$(slot_container "$slot"):8080/g" "$NGINX_TEMPLATE" > "$NGINX_CONF"
  docker exec ogs-swg-proxy nginx -t >/dev/null
  docker exec ogs-swg-proxy nginx -s reload >/dev/null
  printf '%s' "$slot" > "$ACTIVE_FILE"
}

safe_stop_slot() {
  local slot="$1"
  if slot_running "$slot"; then
    docker stop "$(slot_container "$slot")" >/dev/null || true
    log_event "gc stopped slot=${slot}"
  fi
}

ensure_previous_is_stopped_after_bake() {
  local now active previous bake_until
  now="$(date +%s)"
  [ -f "$PREVIOUS_FILE" ] || return 0
  [ -f "$BAKE_UNTIL_FILE" ] || return 0

  active="$(cat "$ACTIVE_FILE" 2>/dev/null || true)"
  previous="$(cat "$PREVIOUS_FILE" 2>/dev/null || true)"
  bake_until="$(cat "$BAKE_UNTIL_FILE" 2>/dev/null || echo 0)"

  case "$bake_until" in
    ''|*[!0-9]*) bake_until=0 ;;
  esac

  if [ "$now" -ge "$bake_until" ] && [ -n "$previous" ] && [ "$previous" != "$active" ]; then
    safe_stop_slot "$previous"
  fi
}

rollback_to_previous() {
  local active previous
  active="$(cat "$ACTIVE_FILE" 2>/dev/null || true)"
  previous="$(cat "$PREVIOUS_FILE" 2>/dev/null || true)"

  if [ -z "$active" ] || [ -z "$previous" ] || [ "$active" = "$previous" ]; then
    return 1
  fi

  if ! slot_exists "$previous"; then
    log_event "rollback skipped reason=previous_slot_missing active=${active} previous=${previous}"
    return 1
  fi

  if ! slot_running "$previous"; then
    docker start "$(slot_container "$previous")" >/dev/null || true
    sleep 2
  fi

  for _ in $(seq 1 20); do
    if slot_health_ok "$previous"; then
      switch_proxy_to "$previous"
      printf '%s' "$active" > "$PREVIOUS_FILE"
      printf '%s' "$(( $(date +%s) + 300 ))" > "$BAKE_UNTIL_FILE"
      log_event "incident recovered rollback_from=${active} rollback_to=${previous}"
      return 0
    fi
    sleep 2
  done

  log_event "rollback failed active=${active} previous=${previous}"
  return 1
}

main_loop() {
  local failures=0 active
  log_event "watchdog started"

  while true; do
    if [ ! -f "$ACTIVE_FILE" ]; then
      sleep "$CHECK_INTERVAL_SEC"
      continue
    fi

    active="$(cat "$ACTIVE_FILE" 2>/dev/null || true)"
    if [ "$active" != "blue" ] && [ "$active" != "green" ]; then
      sleep "$CHECK_INTERVAL_SEC"
      continue
    fi

    ensure_previous_is_stopped_after_bake

    if slot_health_ok "$active"; then
      failures=0
    else
      failures=$((failures + 1))
      log_event "active_health_failed slot=${active} failures=${failures}"
      if [ "$failures" -ge "$FAIL_THRESHOLD" ]; then
        if rollback_to_previous; then
          failures=0
        fi
      fi
    fi

    sleep "$CHECK_INTERVAL_SEC"
  done
}

main_loop
