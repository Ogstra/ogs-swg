#!/usr/bin/env bash
set -euo pipefail

SKIP_BACKEND=false
SKIP_FRONTEND=false

for arg in "$@"; do
  case "$arg" in
    --skip-backend) SKIP_BACKEND=true ;;
    --skip-frontend) SKIP_FRONTEND=true ;;
  esac
done

assert_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Error: '$1' no está en PATH" >&2
    exit 1
  fi
}

if [ "$SKIP_BACKEND" = false ]; then
  assert_cmd go
fi
if [ "$SKIP_FRONTEND" = false ]; then
  assert_cmd npm
fi

GO_MOD_FLAG=""
if [ -d vendor ]; then
  GO_MOD_FLAG="-mod=vendor"
fi

if [ "$SKIP_BACKEND" = false ]; then
  echo ">>> Backend tests (go test ./...)"
  go test $GO_MOD_FLAG ./...
  echo ">>> Backend build (ogs-swg)"
  go build $GO_MOD_FLAG -o ogs-swg main.go
fi

if [ "$SKIP_FRONTEND" = false ]; then
  echo ">>> Frontend deps (npm ci)"
  (
    cd frontend
    npm ci
    echo ">>> Frontend build (npm run build)"
    npm run build
  )
fi

echo "✓ Todo OK"
