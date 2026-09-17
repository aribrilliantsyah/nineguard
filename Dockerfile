# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o nineguard cmd/nineguard/main.go

# Run stage
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app
COPY --from=builder /app/nineguard /app/nineguard

RUN mkdir -p /data && chown -R appuser:appgroup /data /app

USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]

ENV NINEGUARD_PORT=8080 \
    NINEGUARD_AUTH_ENABLED=true \
    NINEGUARD_DB_FILE=/data/nineguard.db \
    NINEGUARD_AUTH_FILE=/data/auth.json

ENTRYPOINT ["/app/nineguard"]
