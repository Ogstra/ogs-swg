#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

TEST_ROOT="${OGS_TEST_DATA_DIR:-$ROOT_DIR/data/wg-test-mode}"
mkdir -p "$TEST_ROOT/wireguard"

export OGS_WIREGUARD_TEST_MODE="${OGS_WIREGUARD_TEST_MODE:-true}"
export OGS_WIREGUARD_CONFIG_DIR="${OGS_WIREGUARD_CONFIG_DIR:-$TEST_ROOT/wireguard}"
export OGS_WIREGUARD_CONFIG_PATH="${OGS_WIREGUARD_CONFIG_PATH:-$OGS_WIREGUARD_CONFIG_DIR/wg0.conf}"
export OGS_DB_PATH="${OGS_DB_PATH:-$TEST_ROOT/stats.db}"
export OGS_LISTEN_ADDR="${OGS_LISTEN_ADDR:-:18080}"

CONFIG_PATH="${OGS_CONFIG_PATH:-config.json}"

echo "Starting OGS-SWG in WireGuard test mode"
echo "  config: $CONFIG_PATH"
echo "  listen: $OGS_LISTEN_ADDR"
echo "  wireguard dir: $OGS_WIREGUARD_CONFIG_DIR"
echo "  db: $OGS_DB_PATH"

exec go run ./cmd/server -config "$CONFIG_PATH" --wg-test-mode "$@"
