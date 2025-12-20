#!/usr/bin/env bash
set -euo pipefail

# Build script for Linux that prefers local toolchains in .tools/
# Flags:
#   --skip-backend      Skip Go tests/build
#   --skip-frontend     Skip frontend build
#   --skip-tests        Skip Go tests (still builds)

SKIP_BACKEND=false
SKIP_FRONTEND=false
SKIP_TESTS=false

for arg in "$@"; do
  case "$arg" in
    --skip-backend) SKIP_BACKEND=true ;;
    --skip-frontend) SKIP_FRONTEND=true ;;
    --skip-tests) SKIP_TESTS=true ;;
    *) echo "Unknown flag: $arg" >&2; exit 1 ;;
  esac
done

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$ROOT_DIR/build"

pick_bin() {
  local candidate="$1"
  shift
  if [[ -x "$candidate" ]]; then
    echo "$candidate"
    return 0
  fi
  command -v "$1" 2>/dev/null || return 1
}

GO_BIN="$(pick_bin "$ROOT_DIR/.tools/go/bin/go" go || true)"
NPM_BIN="$(pick_bin "$ROOT_DIR/.tools/node/bin/npm" npm || true)"

if [[ "$SKIP_BACKEND" != true && -z "$GO_BIN" ]]; then
  echo "Go not found (looked in .tools/go/bin and PATH)" >&2
  exit 1
fi
if [[ "$SKIP_FRONTEND" != true && -z "$NPM_BIN" ]]; then
  echo "npm not found (looked in .tools/node/bin and PATH)" >&2
  exit 1
fi

GO_MOD_FLAG=""
if [[ -d "$ROOT_DIR/vendor" ]]; then
  GO_MOD_FLAG="-mod=vendor"
fi

if [[ "$SKIP_BACKEND" != true ]]; then
  if [[ "$SKIP_TESTS" != true ]]; then
    echo ">>> Backend tests (go test ./...)"
    "$GO_BIN" test $GO_MOD_FLAG ./...
  else
    echo ">>> Backend tests skipped"
  fi

  GOOS="$("$GO_BIN" env GOOS)"
  GOARCH="$("$GO_BIN" env GOARCH)"
  BIN_NAME="ogs-swg-${GOOS}-${GOARCH}"
  mkdir -p "$BUILD_DIR"
  echo ">>> Backend build (${BIN_NAME})"
  CGO_ENABLED=0 "$GO_BIN" build $GO_MOD_FLAG -o "$BUILD_DIR/$BIN_NAME" "$ROOT_DIR/main.go"
fi

if [[ "$SKIP_FRONTEND" != true ]]; then
  echo ">>> Frontend deps (npm ci)"
  pushd "$ROOT_DIR/frontend" >/dev/null
  NODE_OPTIONS="--max-old-space-size=512" "$NPM_BIN" ci
  echo ">>> Frontend build (npm run build)"
  NODE_OPTIONS="--max-old-space-size=512" "$NPM_BIN" run build
  popd >/dev/null
  if [[ -d "$ROOT_DIR/frontend/dist" ]]; then
    echo ">>> Copying frontend build to $BUILD_DIR/frontend"
    rm -rf "$BUILD_DIR/frontend"
    mkdir -p "$BUILD_DIR/frontend"
    cp -R "$ROOT_DIR/frontend/dist/"* "$BUILD_DIR/frontend/"
  fi
fi

echo "DONE - Build finished."
