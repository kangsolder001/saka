SHELL := /bin/bash
.DEFAULT_GOAL := help

GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint
GOOSE := go tool goose # only used to scaffold new migration files
MIGRATIONS_DIR := migrations

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

##@ Local services

.PHONY: config
config: ## Create config.json from config.example.json (only if missing)
	@test -f config.json && echo "config.json already exists" || (cp config.example.json config.json && echo "created config.json")

.PHONY: up
up: ## Start PostgreSQL and Mailpit (docker compose)
	docker compose up -d --wait

.PHONY: down
down: ## Stop local services
	docker compose down

##@ Run

.PHONY: run
run: ## Run the API
	go run ./cmd/api

.PHONY: run-worker
run-worker: ## Run the background worker (outbox / email)
	go run ./cmd/worker

.PHONY: build
build: ## Build binaries into ./bin
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	go build -o bin/migrate ./cmd/migrate

##@ Database

.PHONY: migrate
migrate: ## Apply all pending migrations
	go run ./cmd/migrate up

.PHONY: migrate-down
migrate-down: ## Roll back the last migration
	go run ./cmd/migrate down

.PHONY: migrate-status
migrate-status: ## Show migration status
	go run ./cmd/migrate status

.PHONY: migrate-new
migrate-new: ## Create a migration: make migrate-new name=create_orgs
	@test -n "$(name)" || (echo "usage: make migrate-new name=<snake_case_name>" && exit 1)
	$(GOOSE) -dir $(MIGRATIONS_DIR) create -s $(name) sql

.PHONY: seed
seed: ## Check the default plans and create the demo account (dev only, idempotent)
	go run ./cmd/seed

.PHONY: loadtest
loadtest: ## Light load test of a running dev API: make loadtest ARGS="-scenario login -c 8 -n 400" (see docs/loadtest.md)
	go run ./cmd/loadtest $(ARGS)

##@ Code generation

.PHONY: sqlc
sqlc: ## Generate Go code from SQL queries
	@if grep -qE '^  - engine:' sqlc.yaml; then go tool sqlc generate; else echo "sqlc: no modules configured yet, skipping"; fi

.PHONY: mocks
mocks: ## Generate mocks (mockery)
	go tool mockery

##@ Quality

.PHONY: test
test: ## Unit tests (no Docker needed)
	go test -race ./...

.PHONY: test-integration
test-integration: ## Unit + integration tests (needs Docker for testcontainers)
	go test -race -tags integration ./...

$(GOLANGCI_LINT):
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...
	$(GOLANGCI_LINT) run --build-tags integration ./...

.PHONY: tidy
tidy: ## Tidy go.mod
	go mod tidy

##@ Frontend Web

.PHONY: web-install
web-install: ## Install frontend dependencies
	pnpm --dir web install --frozen-lockfile

.PHONY: web-dev
web-dev: ## Run frontend dev server (port 3000, proxies /api to :8080)
	pnpm --dir web dev

.PHONY: web-build
web-build: ## Build frontend SPA
	pnpm --dir web build

.PHONY: web-test
web-test: ## Run frontend unit tests
	pnpm --dir web test

.PHONY: web-lint
web-lint: ## Run frontend format check and linter
	pnpm --dir web lint

.PHONY: web-api
web-api: ## Generate TypeScript types from docs/openapi.yaml
	pnpm --dir web api:generate

.PHONY: web-e2e
web-e2e: ## Run frontend E2E tests
	pnpm --dir web test:e2e

.PHONY: web-check
web-check: ## Run lint, type-check, test, and build for frontend
	pnpm --dir web lint
	pnpm --dir web check
	pnpm --dir web test
	pnpm --dir web build

