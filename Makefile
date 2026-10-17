SHELL := /bin/bash
.DEFAULT_GOAL := help

# Load .env (if present) so run/migrate targets see DATABASE_URL, GOOSE_*, etc.
-include .env
export

GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := bin/golangci-lint
GOOSE := go tool goose
MIGRATIONS_DIR := migrations

.PHONY: help
help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

##@ Local services

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
