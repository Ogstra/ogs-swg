#!/bin/bash
set -e

# Colors
GREEN='\033[0;32m'
NC='\033[0m'

echo -e "${GREEN}>>> Step 1: Building Backend (Go)...${NC}"
# Download dependencies to ./vendor directory (local to project)
echo "    Vendoring dependencies..."
go mod vendor
# Build using local vendor directory
echo "    Building binary..."
GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"
BIN_NAME="ogs-swg-${GOOS}-${GOARCH}"
BUILD_DIR="./build"
mkdir -p "$BUILD_DIR"
go build -mod=vendor -o "$BUILD_DIR/$BIN_NAME" main.go

echo -e "${GREEN}>>> Step 2: Building Frontend (React)...${NC}"
cd frontend
# Check if node_modules exists to save time, but install if missing
if [ ! -d "node_modules" ]; then
    echo "    Installing npm dependencies locally..."
    npm install
fi
echo "    Building static assets..."
npm run build
cd ..
if [ -d frontend/dist ]; then
    echo "    Copying frontend build to $BUILD_DIR/frontend..."
    rm -rf "$BUILD_DIR/frontend"
    mkdir -p "$BUILD_DIR"
    cp -R frontend/dist "$BUILD_DIR/frontend"
fi

echo -e "${GREEN}>>> Step 3: Preparing Environment...${NC}"
# Use a temporary DB for testing to not overwrite production data immediately
TEST_DB="./test_stats.db"

# Check if config and log exist (assuming running on VPS)
SINGBOX_CONFIG="/etc/sing-box/config.json"
ACCESS_LOG="/var/log/singbox.log"

if [ ! -f "$SINGBOX_CONFIG" ]; then
    echo "Warning: $SINGBOX_CONFIG not found. Using default paths may fail."
fi

echo -e "${GREEN}>>> Step 4: Starting OGS-SWG...${NC}"
echo "-----------------------------------------------------"
echo "App running at: http://$(curl -s ifconfig.me):8080"
echo "Press Ctrl+C to stop."
echo "-----------------------------------------------------"

./"$BUILD_DIR/$BIN_NAME" \
  --config "./config.json" \
  --singbox-config "$SINGBOX_CONFIG" \
  --log "$ACCESS_LOG" \
  --db "$TEST_DB"
