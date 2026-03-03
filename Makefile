.PHONY: build test test-unit lint vet fmt \
       run run-local run-gateway \
       generate clean help

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
CLIENT_DIR  ?= $(CURDIR)/../overload-party-client
BATTLE_DIR  ?= $(CURDIR)/../overload-party-battle

# ─── Code Generation ────────────────────────────────────
generate:  ## Generate cards.json, constants, cardno_gen.go, CARDS.md
	python3 $(COMMON_DIR)/scripts/generate_from_yaml.py \
		--gateway-dir $(CURDIR) \
		--battle-dir $(BATTLE_DIR) \
		--client-dir $(CLIENT_DIR)

# ─── Build ───────────────────────────────────────────────
build:  ## Build Docker image
	docker build -t $(APP) .

# ─── Test & Lint ─────────────────────────────────────────
test: test-unit  ## Run all tests

test-unit:  ## Run unit tests
	go test ./internal/... -count=1 -race

lint:  ## Run golangci-lint
	golangci-lint run ./...

vet:  ## Run go vet
	go vet ./...

fmt:  ## Format code
	goimports -w -local $(MODULE) .

# ─── Run ─────────────────────────────────────────────────
run: run-local  ## Run gateway server (alias)

run-local:  ## Run local gateway server (no DB/Firebase, in-memory mock repos)
	go run ./cmd/local

run-gateway:  ## Run gateway server (PostgreSQL mode)
	DATABASE_URL="postgresql://dev:dev@localhost:5432/overload_party" \
	ENV=dev \
	go run ./cmd/main

# ─── Misc ────────────────────────────────────────────────
clean:  ## Remove build artifacts
	rm -rf bin/ local

help:  ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
