# Build and verification entry points for V2

SHELL := /bin/bash
.DEFAULT_GOAL := help

GO        ?= go
PNPM      ?= pnpm
WEB_DIR   := web
DATA_DIR  ?= $(CURDIR)/.cache/runtime
CONTENT_DIR ?= $(CURDIR)/content
BACKUP_DIR  ?= $(CURDIR)/.cache/backups

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'


.PHONY: embed-spa
embed-spa: ## Copy the built SPA into the Go embed directory
	@if [ ! -f $(WEB_DIR)/dist/index.html ]; then \
		echo "no SPA build at $(WEB_DIR)/dist; run 'make web-build' first"; exit 1; fi
	@rm -rf internal/spafs/dist
	@mkdir -p internal/spafs/dist
	@touch internal/spafs/dist/.gitkeep
	@cp -R $(WEB_DIR)/dist/. internal/spafs/dist/
	@echo "embedded SPA: $$(find internal/spafs/dist -type f | wc -l | tr -d ' ') files"

.PHONY: build
build: embed-spa ## Build the Go server (with embedded SPA) and content tool
	$(GO) build ./...
	$(GO) build -o bin/uma-server ./cmd/server
	$(GO) build -o bin/uma-content ./cmd/content

.PHONY: embed-spa-check
embed-spa-check: ## Fail when internal/spafs/dist is stale versus web/dist
	@if [ ! -f $(WEB_DIR)/dist/index.html ]; then \
		echo "embed-spa-check: no SPA build at $(WEB_DIR)/dist; run 'make web-build' first"; exit 1; fi
	@if ! diff -r -q --exclude=.gitkeep $(WEB_DIR)/dist internal/spafs/dist >/dev/null 2>&1; then \
		echo "embed-spa-check: internal/spafs/dist does not match $(WEB_DIR)/dist."; \
		echo "  The binary would embed a stale or placeholder SPA."; \
		echo "  Fix: make embed-spa"; \
		exit 1; fi
	@echo "embed-spa-check: embedded SPA matches $(WEB_DIR)/dist"

.PHONY: build-server-only
build-server-only: ## Build the server without rebuilding the embedded SPA
	$(GO) build ./...
	$(GO) build -o bin/uma-server ./cmd/server

.PHONY: vet
vet: ## Run go vet on every package
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format Go sources
	gofmt -w cmd internal

.PHONY: fmt-check
fmt-check: ## Fail if any Go source is unformatted
	@out=$$(gofmt -l cmd internal); \
	if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

.PHONY: test
test: ## Run Go tests
	$(GO) test ./... -count=1

.PHONY: test-race
test-race: ## Run Go tests under the race detector
	$(GO) test ./... -count=1 -race

.PHONY: cover
cover: ## Run Go tests with a coverage report
	$(GO) test ./... -count=1 -coverprofile=coverage.out
	$(GO) tool cover -func=coverage.out | tail -1


.PHONY: web-install
web-install: ## Install frontend dependencies AND the isolated toolchain from their lockfiles
	$(PNPM) --dir $(WEB_DIR) install --frozen-lockfile
	$(PNPM) --dir $(WEB_DIR)/tools/toolchain install --frozen-lockfile

.PHONY: web-toolchain-install
web-toolchain-install: ## Install only the isolated toolchain (ESLint/TS6/openapi-typescript)
	$(PNPM) --dir $(WEB_DIR)/tools/toolchain install --frozen-lockfile

.PHONY: web-build
web-build: ## Build the SPA into web/dist
	$(PNPM) --dir $(WEB_DIR) run build

.PHONY: web-typecheck
web-typecheck: ## Type-check the SPA
	$(PNPM) --dir $(WEB_DIR) run typecheck

.PHONY: web-lint
web-lint: web-toolchain-install ## Lint the SPA (installs the isolated toolchain first)
	$(PNPM) --dir $(WEB_DIR) run lint

.PHONY: web-test
web-test: ## Run frontend tests
	$(PNPM) --dir $(WEB_DIR) run test

.PHONY: web-copy-check
web-api-constants: ## Print the exact constant values API calls need (kinds, tiePolicy, phases)
	@# Read action constants from source rather than duplicating API values
	node $(WEB_DIR)/scripts/audit-api-constants.mjs

.PHONY: web-copy-check
web-copy-check: ## Fail when player-facing copy contains internal or execution-location wording
	$(PNPM) --dir $(WEB_DIR) run copy:check

.PHONY: web-api-types
web-api-types: web-toolchain-install ## Regenerate API types from the OpenAPI contract
	$(PNPM) --dir $(WEB_DIR) run api:types

.PHONY: web-size
web-size: ## Check the built SPA against the bundle size budgets
	$(PNPM) --dir $(WEB_DIR) run size


.PHONY: content-compile
content-compile: ## Compile canonical data into versioned packs
	$(GO) run ./cmd/content build --out $(CONTENT_DIR)

.PHONY: content-verify
content-verify: ## Verify the published pack manifest and checksums
	@# Resolve the current pointer because verify expects a pack directory
	@pack=$$(cat $(CONTENT_DIR)/current 2>/dev/null); \
	if [ -z "$$pack" ]; then \
		echo "content-verify: no 'current' pointer under $(CONTENT_DIR)"; exit 1; \
	fi; \
	if [ ! -d "$(CONTENT_DIR)/$$pack" ]; then \
		echo "content-verify: 'current' points at $$pack which does not exist"; exit 1; \
	fi; \
	echo "content-verify: $$pack"; \
	$(GO) run ./cmd/content verify --dir $(CONTENT_DIR)/$$pack


.PHONY: contract-check
contract-check: ## Validate the OpenAPI contract and check generated types are current
	$(GO) run ./cmd/content contract --spec api/openapi.json
	@# Compare file contents so untracked generated files are checked too
	@before=$$(shasum -a 256 $(WEB_DIR)/src/lib/api/schema.d.ts 2>/dev/null | cut -d' ' -f1); \
	$(PNPM) --dir $(WEB_DIR) run api:types >/dev/null; \
	after=$$(shasum -a 256 $(WEB_DIR)/src/lib/api/schema.d.ts 2>/dev/null | cut -d' ' -f1); \
	if [ "$$before" != "$$after" ]; then \
		echo "generated API types are stale; the regenerated file differs from what was on disk"; \
		exit 1; \
	fi; \
	if [ -z "$$after" ]; then \
		echo "could not read $(WEB_DIR)/src/lib/api/schema.d.ts after regeneration"; \
		exit 1; \
	fi


.PHONY: check
check: fmt-check vet test test-race web-typecheck web-lint web-copy-check web-api-constants web-test web-build web-size embed-spa-check content-verify ## Every gate CI runs

.PHONY: ci
ci: check contract-check ## CI entry point


.PHONY: dev
dev: ## Run the server against a local scratch database
	@# Preserve caller overrides when supplying local defaults
	UMA_ADDR="$${UMA_ADDR:-:8080}" \
	UMA_DATA_DIR="$${UMA_DATA_DIR:-$(DATA_DIR)}" \
	UMA_CONTENT_DIR="$${UMA_CONTENT_DIR:-$(CONTENT_DIR)}" \
	UMA_BACKUP_DIR="$${UMA_BACKUP_DIR:-$(BACKUP_DIR)}" \
	UMA_PUBLIC_ORIGIN="$${UMA_PUBLIC_ORIGIN:-http://localhost:8080}" \
	$(GO) run ./cmd/server

.PHONY: backup
backup: ## Write a consistent online backup
	@# Preserve caller overrides when supplying local defaults
	UMA_DATA_DIR="$${UMA_DATA_DIR:-$(DATA_DIR)}" \
	$(GO) run ./cmd/server backup --dir "$${UMA_BACKUP_DIR:-$(BACKUP_DIR)}"

.PHONY: clean
clean: ## Remove build output and scratch state
	rm -rf bin coverage.out
	rm -rf $(WEB_DIR)/dist
	rm -rf internal/spafs/dist
	mkdir -p internal/spafs/dist
	touch internal/spafs/dist/.gitkeep
	rm -rf .cache/runtime
