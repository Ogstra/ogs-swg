# Stage 1: Build Frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
ARG APP_COMMIT_SHA=local
ENV VITE_APP_COMMIT=${APP_COMMIT_SHA}
RUN npm run build

# Stage 2: Build Backend
FROM golang:1.24-bookworm AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# CGO_ENABLED=1 because we use SQLite (github.com/mattn/go-sqlite3)
RUN CGO_ENABLED=1 GOOS=linux go build -o ogs-swg ./cmd/server

# Stage 3: Final runtime image
FROM debian:bookworm-slim

# Install basic runtime dependencies
# - ca-certificates for HTTPS
# - tzdata for timezones
# - iproute2 for ip command (optional debug)
# - curl/wget for healthcheck
# - systemd package for systemd-run/systemctl/journalctl binaries (docker_local mode)
RUN apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    iproute2 \
    curl \
    systemd \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy artifacts
COPY --from=backend-builder /app/ogs-swg /app/ogs-swg
COPY --from=frontend-builder /app/dist /app/frontend

# Create directory for data
RUN mkdir -p /app/data

# Environment variables
ENV OGS_LISTEN_ADDR=":8080"
ENV OGS_DB_PATH="/app/data/stats.db"

# Expose port
EXPOSE 8080

# Entrypoint
CMD ["/app/ogs-swg", "-config", "/app/data/config.json"]
