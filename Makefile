.PHONY: build test test-unit test-integration test-emulator-up test-emulator-down lint vet fmt \
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
FIRESTORE_EMULATOR_HOST ?= localhost:8080
FIRESTORE_EMULATOR_CONTAINER := gateway-firestore-emulator

test: test-unit  ## Run unit tests only (no Docker required)

test-all: test-unit test-integration  ## Run unit + integration tests

test-unit:  ## Run unit tests
	go test ./internal/... -count=1 -race

test-integration: test-emulator-up  ## Run integration tests (requires Docker)
	FIRESTORE_EMULATOR_HOST="$(FIRESTORE_EMULATOR_HOST)" \
	GOOGLE_CLOUD_PROJECT_ID=overload-party-test \
	go test -race -tags=integration ./internal/... -count=1 -v

test-emulator-up:  ## Start Firestore emulator container (reuses one already listening)
	@if curl -sf http://$(FIRESTORE_EMULATOR_HOST) >/dev/null 2>&1; then \
		echo "Firestore emulator already running at $(FIRESTORE_EMULATOR_HOST)"; \
	else \
		docker run -d --rm --name $(FIRESTORE_EMULATOR_CONTAINER) -p 8080:8080 \
			gcr.io/google.com/cloudsdktool/google-cloud-cli:emulators \
			gcloud beta emulators firestore start --project=overload-party-test --host-port=0.0.0.0:8080; \
		for i in $$(seq 1 30); do \
			curl -sf http://$(FIRESTORE_EMULATOR_HOST) >/dev/null && break; sleep 1; \
		done; \
		curl -sf http://$(FIRESTORE_EMULATOR_HOST) >/dev/null || { echo "Firestore emulator failed to start"; exit 1; }; \
	fi

test-emulator-down:  ## Stop Firestore emulator container
	docker stop $(FIRESTORE_EMULATOR_CONTAINER)

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
