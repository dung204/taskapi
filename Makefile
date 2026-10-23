# Dev tasks for taskapi. Recipe lines MUST start with a TAB (see .editorconfig).

SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

# Load .env and export it to every recipe. Lines must be plain KEY=VALUE:
# no quotes, no spaces around "=". Anything fancier belongs in the shell.
ifneq (,$(wildcard .env))
include .env
export
endif

# --- Pinned tools ------------------------------------------------------------
# Go-based tools (goose, sqlc, oapi-codegen) are pinned in go.mod and run with
# `go tool <name>`. golangci-lint is NOT installed that way (its docs advise
# against it), so it is downloaded as a binary into bin/. Bump the version
# below and `make lint` reinstalls it.
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT         := bin/golangci-lint
GOLANGCI_LINT_STAMP   := bin/.golangci-lint-$(GOLANGCI_LINT_VERSION)

SQUAWK_VERSION := v2.69.0
SQUAWK         := bin/squawk
SQUAWK_STAMP   := bin/.squawk-$(SQUAWK_VERSION)

MIGRATIONS_DIR := migrations

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  %-16s %s\n", $$1, $$2}' Makefile

# --- Tooling -----------------------------------------------------------------
$(GOLANGCI_LINT_STAMP):
	@mkdir -p bin
	@rm -f bin/.golangci-lint-*
	curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b bin $(GOLANGCI_LINT_VERSION)
	@touch $@

$(SQUAWK_STAMP):
	@mkdir -p bin
	@rm -f bin/.squawk-*
	curl -sSfL -o bin/squawk --create-dirs https://github.com/sbdchd/squawk/releases/download/$(SQUAWK_VERSION)/squawk-linux-x64
	chmod +x bin/squawk
	@touch $@

.PHONY: tools
tools: $(GOLANGCI_LINT_STAMP) $(SQUAWK_STAMP) ## Install the binary tools (golangci-lint, squawk) into bin/

.PHONY: hooks
hooks: ## Install the git hooks
	go tool lefthook install

# --- Code quality ------------------------------------------------------------
.PHONY: fmt
fmt: $(GOLANGCI_LINT_STAMP) ## Format code (formatters from .golangci.yml)
	$(GOLANGCI_LINT) fmt ./...

.PHONY: fmt-check
fmt-check: $(GOLANGCI_LINT_STAMP) ## Fail if any file is not formatted (no changes written)
	$(GOLANGCI_LINT) fmt --diff ./...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: lint
lint: $(GOLANGCI_LINT_STAMP) ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-fix
lint-fix: $(GOLANGCI_LINT_STAMP) ## Run golangci-lint and apply auto-fixes
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: lint-sql
lint-sql: $(GOLANGCI_LINT_STAMP) ## Run squawk to lint SQL migrations
	$(SQUAWK) $(MIGRATIONS_DIR)/*.sql

# --- Tests -------------------------------------------------------------------
.PHONY: test
test: ## Run tests (race detector, shuffled; Postgres tests skip without TEST_DATABASE_URL)
	go test -race -shuffle=on ./...

.PHONY: test-db
test-db: ## Run tests including Postgres store (TRUNCATES the tasks table of TEST_DATABASE_URL!)
	@test -n "$${TEST_DATABASE_URL:-}" || { echo "TEST_DATABASE_URL is not set (use a dedicated database)."; exit 1; }
	go test -race -shuffle=on -count=1 ./...

.PHONY: cover
cover: ## Run tests and print coverage per function
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

.PHONY: check
check: vet lint test ## Everything CI should run

# --- Build and run -----------------------------------------------------------
.PHONY: build
build: ## Build the server into bin/server
	go build -o bin/server ./cmd/server

.PHONY: run
run: ## Run the server (reads .env if present)
	go run ./cmd/server

.PHONY: gen
gen: ## Run go generate (sqlc, oapi-codegen, ... via //go:generate)
	go generate ./...

.PHONY: gen-sql
gen-sql: ## Run sqlc to generate Go code from SQL queries
	go tool sqlc generate

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

# --- Migrations (goose via `go tool`; needs DATABASE_URL) ----------------------
# One-time setup:  go get -tool github.com/pressly/goose/v3/cmd/goose@<version>
.PHONY: _require-db
_require-db:
	@test -n "$${DATABASE_URL:-}" || { echo "DATABASE_URL is not set."; exit 1; }

.PHONY: migrate-up
migrate-up: _require-db ## Apply all pending migrations
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" up

.PHONY: migrate-down
migrate-down: _require-db ## Roll back the last migration
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" down

.PHONY: migrate-status
migrate-status: _require-db ## Show migration status
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$$DATABASE_URL" status

.PHONY: migrate-create
migrate-create: ## Create a migration: make migrate-create name=add_index
	@test -n "$(name)" || { echo "Usage: make migrate-create name=<migration_name>"; exit 1; }
	go tool goose -dir $(MIGRATIONS_DIR) create $(name) sql

# --- Cleanup -----------------------------------------------------------------
.PHONY: clean
clean: ## Remove build outputs and coverage files (keeps installed tools)
	rm -f bin/server coverage.out
