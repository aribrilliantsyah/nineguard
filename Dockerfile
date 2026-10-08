# Build stage
FROM golang:1.27-alpine AS builder

ARG VERSION=v1.1.0
ARG COMMIT=dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -tags server \
    -ldflags="-s -w -X 'nineguard/internal/version.Version=${VERSION}' -X 'nineguard/internal/version.Commit=${COMMIT}'" \
    -o nineguard cmd/nineguard/main.go

# Run stage
FROM alpine:3.20

ARG VERSION=v1.1.0
ARG COMMIT=dev

LABEL org.opencontainers.image.title="NineGuard" \
      org.opencontainers.image.description="Universal OpenAI-Compatible Gateway, Model Firewall & Multi-Provider Router" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/DarkTama/nineguard"

RUN apk --no-cache add ca-certificates tzdata && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

WORKDIR /app
COPY --from=builder /app/nineguard /app/nineguard

RUN mkdir -p /data && chown -R appuser:appgroup /data /app

USER 10001:10001
EXPOSE 8080
VOLUME ["/data"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:${NINEGUARD_PORT}/healthz" >/dev/null || exit 1

ENV NINEGUARD_PORT=8080 \
    NINEGUARD_AUTH_ENABLED=true \
    NINEGUARD_DB_FILE=/data/nineguard.db \
    NINEGUARD_AUTH_FILE=/data/auth.json

ENTRYPOINT ["/app/nineguard"]
