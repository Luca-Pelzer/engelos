# engelOS — multi-stage Dockerfile
#
# Stage 1: build the Go daemon with CGO disabled (pure-Go SQLite).
# Stage 2: copy the binary into a distroless container for minimal attack
# surface.
#
# Final image is ~10 MB and runs as non-root user `nonroot` (uid 65532).

# syntax=docker/dockerfile:1.7

# -------- Builder --------
FROM golang:1.24-alpine AS builder

WORKDIR /src

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download -x

# Copy the rest of the source.
COPY . .

ARG VERSION=0.0.0-docker
ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
        -trimpath \
        -ldflags="-s -w -X main.Version=${VERSION}" \
        -o /out/engelos \
        ./cmd/engelos

# -------- Runtime --------
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="engelOS"
LABEL org.opencontainers.image.description="Open-source streaming bot — daemon"
LABEL org.opencontainers.image.source="https://github.com/engelos-bot/engelos"
LABEL org.opencontainers.image.licenses="AGPL-3.0-only"
LABEL org.opencontainers.image.vendor="engelOS contributors"

COPY --from=builder /out/engelos /usr/local/bin/engelos

# Data directory for SQLite + state. Mount a volume here in production.
VOLUME ["/data"]
WORKDIR /data

# Default port for the local HTTP/WS API. Set ENGELOS_ADDR=0.0.0.0:8080 to bind
# to all interfaces (only do this behind a reverse proxy with TLS + auth).
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/engelos"]
