#!/usr/bin/env bash
set -euo pipefail

# Simple build helper to produce binaries for macOS (host) and Linux.
# Usage: ./build_macos_linux.sh
# Env vars:
#   TARGETS="darwin/arm64 linux/amd64"  # override target matrix
#   OUTPUT_DIR="./dist"                  # where binaries are placed
#   SKIP_FRONTEND=1                     # skip npm build if already built

assert_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: '$1' not found in PATH" >&2
    exit 1
  fi
}

assert_cmd go
assert_cmd npm

OUTPUT_DIR="${OUTPUT_DIR:-./dist}"
TARGETS="${TARGETS:-darwin/arm64 linux/amd64}"

GO_MOD_FLAG=""
if [ -d vendor ]; then
  GO_MOD_FLAG="-mod=vendor"
fi

mkdir -p "$OUTPUT_DIR"

if [ "${SKIP_FRONTEND:-0}" != "1" ]; then
  echo ">>> Building frontend (npm ci && npm run build)"
  (cd frontend && npm ci && npm run build)
else
  echo ">>> Skipping frontend (SKIP_FRONTEND=1)"
fi

echo ">>> Building backend for targets: $TARGETS"
for target in $TARGETS; do
  GOOS="${target%/*}"
  GOARCH="${target#*/}"
  BIN="${OUTPUT_DIR}/ogs-swg-${GOOS}-${GOARCH}"
  echo "  -> $GOOS/$GOARCH => $BIN"
  GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED=0 go build $GO_MOD_FLAG -o "$BIN" -ldflags="-s -w" main.go
done

echo "✓ Builds ready in $OUTPUT_DIR"
