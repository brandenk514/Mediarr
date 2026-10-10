# ---- dev loop --------------------------------------------------------------
#   make dev      # build + run everything (app + postgres + redis)
#   make test     # unit + integration (Docker required)
#   make image    # build the Docker test image
#   make lint     # go vet + gofmt check
#   make scan     # gosec / govulncheck (if installed)
#   make clean    # remove local build artifacts
# -------------------------------------------------

GO ?= go
DOCKER ?= docker
APP_NAME := mediarr
IMAGE ?= mediarr
TAG ?= dev

# Version stamping (overridable: make image VERSION=1.2.3)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Compile the service binary into ./bin
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildTime=$(BUILD_DATE)" -o bin/$(APP_NAME) ./cmd/mediarr

.PHONY: run
run: ## Run locally (needs env from .env or environment)
	./bin/$(APP_NAME)

.PHONY: test
test: ## Unit + integration tests (integration needs Docker)
	$(GO) test ./... -count=1 -timeout 300s

.PHONY: test-unit
test-unit: ## Unit tests only (no Docker)
	$(GO) test ./internal/api/... ./internal/auth/... ./internal/config/... ./internal/health/... ./internal/domains/... ./internal/services/... ./internal/secrets/... ./internal/indexers/... ./internal/downloads/... -count=1 -cover

.PHONY: cover
cover: ## Test with coverage report
	$(GO) test ./... -count=1 -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: ## go vet + gofmt check
	$(GO) vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:"; gofmt -l .; exit 1)

.PHONY: tidy
tidy: ## Sync go.mod / go.sum
	$(GO) mod tidy
	$(GO) mod verify

.PHONY: scan
scan: ## Security scans (installs tools if missing)
	@command -v govulncheck >/dev/null || $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...
	@command -v gosec >/dev/null || echo "gosec not installed — skipping"
	@command -v gosec >/dev/null && gosec ./... || true

.PHONY: image
image: ## Build the Docker image (test artifact)
	$(DOCKER) build -f deploy/Dockerfile \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t $(IMAGE):$(TAG) .

.PHONY: image-push
image-push: ## Build and push to ghcr (requires GHCR login)
	$(DOCKER) build -f deploy/Dockerfile \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		-t ghcr.io/brandenk514/$(IMAGE):$(TAG) .
	$(DOCKER) push ghcr.io/brandenk514/$(IMAGE):$(TAG)

.PHONY: dev
dev: ## Start the full dev stack (app + postgres + redis) via compose
	$(DOCKER) compose -f deploy/compose.dev.yml up --build --remove-orphans

.PHONY: dev-down
dev-down: ## Stop the dev stack
	$(DOCKER) compose -f deploy/compose.dev.yml down

.PHONY: key
key: ## Generate a new encryption key (32 bytes hex)
	@openssl rand -hex 32

.PHONY: clean
clean: ## Remove local build artifacts
	rm -rf bin coverage.out
