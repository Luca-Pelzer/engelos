# engelOS — Makefile
# Tested on Linux + macOS. Windows users: use WSL or run the equivalent
# go/pnpm commands directly.

GO ?= go
PNPM ?= pnpm
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
LDFLAGS = -s -w -X main.Version=$(VERSION)
BUILD_FLAGS = -trimpath -ldflags="$(LDFLAGS)"

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help.
	@awk 'BEGIN {FS = ":.*##"; printf "engelOS — make targets\n\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

# -------- Go --------

.PHONY: build
build: ## Build the daemon binary (static, CGO disabled).
	CGO_ENABLED=0 $(GO) build $(BUILD_FLAGS) -o bin/engelos ./cmd/engelos

.PHONY: run
run: ## Build & run the daemon.
	$(GO) run ./cmd/engelos

.PHONY: test
test: ## Run unit tests with race detector.
	$(GO) test -race -timeout 5m ./...

.PHONY: test-short
test-short: ## Run unit tests without race (faster).
	$(GO) test -timeout 2m ./...

.PHONY: coverage
coverage: ## Run tests with coverage report.
	$(GO) test -race -coverprofile=coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage written to coverage.html"

.PHONY: vet
vet: ## Run go vet across all packages.
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint (must be installed).
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed: https://golangci-lint.run/usage/install/" >&2; exit 1; }
	golangci-lint run --timeout 5m

.PHONY: fmt
fmt: ## Format Go code.
	gofmt -s -w .
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

.PHONY: tidy
tidy: ## Run go mod tidy.
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build artifacts.
	rm -rf bin/ dist/ coverage.out coverage.html

# -------- Web frontend --------

.PHONY: web-install
web-install: ## Install web/ dependencies.
	cd web && $(PNPM) install

.PHONY: web-dev
web-dev: ## Start web dev server (Vite).
	cd web && $(PNPM) --filter local dev

.PHONY: web-build
web-build: ## Build web/ static output and copy into internal/web/build/ for embedding.
	cd web && $(PNPM) install --frozen-lockfile && $(PNPM) --filter @engelos/local build
	rm -rf internal/web/build
	mkdir -p internal/web/build
	cp -r web/packages/local/build/. internal/web/build/
	touch internal/web/build/.gitkeep

.PHONY: web-check
web-check: ## TypeScript + Svelte check.
	cd web && $(PNPM) --filter local check

# -------- Combined --------

.PHONY: all
all: web-build build ## Build web + daemon.

.PHONY: dev
dev: ## Run daemon and web dev server in parallel (requires GNU parallel or 2 terminals).
	@echo "Run these in separate terminals:"
	@echo "  Terminal 1:  make run"
	@echo "  Terminal 2:  make web-dev"

# -------- Release (via GoReleaser) --------

.PHONY: release-snapshot
release-snapshot: ## Build a snapshot release (no publish).
	goreleaser release --clean --snapshot

.PHONY: release-check
release-check: ## Validate the GoReleaser config.
	goreleaser check

# -------- Docker --------

.PHONY: docker-build
docker-build: ## Build the Docker image.
	docker build -t engelos:$(VERSION) -t engelos:latest .

.PHONY: docker-run
docker-run: ## Run the Docker image.
	docker run --rm -p 8080:8080 -v engelos-data:/data engelos:latest
