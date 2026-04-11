# ── Build stage ───────────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Кэшируем зависимости — пересборка только при изменении go.mod/go.sum
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /risk-service \
    ./cmd/api/main.go

# ── Runtime stage ─────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk --no-cache add wget ca-certificates

WORKDIR /app

COPY --from=builder /risk-service /risk-service
COPY rules/ /app/rules/

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8083/health || exit 1

EXPOSE 8083

ENTRYPOINT ["/risk-service"]
