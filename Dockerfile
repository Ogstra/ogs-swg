# Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend .
RUN npm run build

# Build backend
FROM golang:1.22-alpine AS backend-builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=mod -o /app/ogs-swg .

# Final Stage (runtime)
FROM alpine:3.19
WORKDIR /app

# Minimal runtime deps
RUN apk add --no-cache ca-certificates tzdata sqlite-libs

# Copy binary
COPY --from=backend-builder /app/ogs-swg /usr/local/bin/ogs-swg

# Copy frontend assets
COPY --from=frontend-builder /app/frontend/dist /app/frontend/dist

# Create directories
RUN mkdir -p /var/log/singbox /var/lib/ogs-swg /etc/sing-box /config

EXPOSE 8080

CMD ["/usr/local/bin/ogs-swg"]
