FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /transaction-service ./cmd/api

FROM alpine:3.20
RUN apk --no-cache add ca-certificates wget
COPY --from=builder /transaction-service /transaction-service
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1
EXPOSE 8080
ENTRYPOINT ["/transaction-service"]