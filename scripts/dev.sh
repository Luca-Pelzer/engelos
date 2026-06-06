#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

cat <<'BANNER'
   _____ _   _ ____ _____ _      ____  ____
  | ____| \ | / ___| ____| |    / __ \/ ___|
  |  _| |  \| | |  _|  _| | |   / / / /\__ \
  | |___| |\  | |_| | |___| |__/ /_/ /___) |
  |_____|_| \_|\____|_____|_____\____/|____/

  engelOS dev helper

BANNER

if ! command -v go >/dev/null 2>&1; then
    echo "Go is not installed. Install from https://go.dev/dl/" >&2
    exit 1
fi

GO_VERSION="$(go env GOVERSION)"
echo "Go: ${GO_VERSION}"

echo
echo "  ::  go mod download"
go mod download

echo
echo "  ::  go vet ./..."
go vet ./...

echo
echo "  ::  go build (CGO disabled)"
mkdir -p bin
CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -X main.Version=0.0.1-alpha.1-dev" \
    -o bin/engelos \
    ./cmd/engelos
ls -lh bin/engelos

if [ -d "web" ] && command -v pnpm >/dev/null 2>&1; then
    echo
    echo "  ::  web/ install"
    (cd web && pnpm install --silent) || echo "  web install failed (non-fatal)"
fi

echo
echo "Done."
echo
echo "Run the daemon:"
echo "  ./bin/engelos"
echo
echo "Run tests:"
echo "  go test -race ./..."
echo
echo "Open the dashboard:"
echo "  http://127.0.0.1:8080"
