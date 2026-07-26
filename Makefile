.PHONY: build test test-unit test-integration test-db-up test-db-down lint vet fmt \
       run run-local run-gateway \
       update-common clean help

# ─── Environment ─────────────────────────────────────────
ifneq (,$(wildcard .env))
    include .env
    export
endif

# ─── Config ──────────────────────────────────────────────
APP    := overload-party-gateway
MODULE := github.com/kenyamaneko/$(APP)

# ─── Common Repo ─────────────────────────────────────────
COMMON_DIR  ?= $(CURDIR)/../overload-party-common

# ─── Dependency ──────────────────────────────────────────
# Gateway depends on multiple common sub-modules since ADR-015 Phase 3.
COMMON_PKGS := \
	github.com/kenyamaneko/overload-party-common/packages/game-design-constants \
	github.com/kenyamaneko/overload-party-common/packages/game-logic-constants \
	github.com/kenyamaneko/overload-party-common/packages/ws-constants \
	github.com/kenyamaneko/overload-party-common/packages/card-types \
	github.com/kenyamaneko/overload-party-common/packages/api-client \
	github.com/kenyamaneko/overload-party-common/packages/api-battle-rpc \
	github.com/kenyamaneko/overload-party-common/packages/devdata

update-common:  ## Update common packages to latest and re-vendor
	GOPRIVATE=github.com/kenyamaneko/* go get -u $(addsuffix @latest,$(COMMON_PKGS))
	go mod tidy
	go mod vendor
	@echo "vendor/ updated — don't forget to commit the changes"

# ─── Build ───────────────────────────────────────────────
build:  ## Build Docker image
	docker build -t $(APP) .

# ─── Test & Lint ─────────────────────────────────────────
TEST_DB_URL ?= postgres://testuser:testpass@localhost:5433/testdb?sslmode=disable

test: test-unit  ## Run unit tests only (no Docker required)

test-all: test-unit test-integration  ## Run unit + integration tests

test-unit:  ## Run unit tests
	go test ./internal/... -count=1 -race

test-integration: test-db-up  ## Run integration tests (requires Docker)
	TEST_DB_URL="$(TEST_DB_URL)" go test ./internal/repository/ -run TestPg -count=1 -race -v

test-db-up:  ## Start test PostgreSQL container
	docker compose -f $(COMMON_DIR)/db/docker-compose.test.yml up -d --wait

test-db-down:  ## Stop test PostgreSQL container
	docker compose -f $(COMMON_DIR)/db/docker-compose.test.yml down

lint:  ## Run golangci-lint
	golangci-lint run ./...

vet:  ## Run go vet
	go vet ./...

fmt:  ## Format code
	goimports -w -local $(MODULE) .

# ─── Run ─────────────────────────────────────────────────
run: run-local  ## Run gateway server (alias)

run-local:  ## Run local gateway server (no DB/Firebase, in-memory mock repos)
	-@lsof -ti:9001 | xargs kill 2>/dev/null; true
	go run ./cmd/local

run-gateway:  ## Run gateway server (PostgreSQL mode)
	DATABASE_CONN="postgresql://dev:dev@localhost:5432/overload_party" \
	DATABASE_IAM_AUTH_ENABLED=false \
	ENV=dev \
	go run ./cmd/main

# ─── Misc ────────────────────────────────────────────────
clean:  ## Remove build artifacts
	rm -rf bin/ local

help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
