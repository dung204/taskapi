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

MODULE = $(shell go list -m)

COVER_PROFILE := coverage.out
COVER_MIN ?= 70
COVER_LINK_SHA  ?= $(GITHUB_SHA)
COVER_LINK_BASE ?= $(if $(GITHUB_REPOSITORY),$(GITHUB_SERVER_URL)/$(GITHUB_REPOSITORY)/blob/$(COVER_LINK_SHA))

# These are excluded from test coverage:
# - *.gen.go, sqlcgen: Generated code
# - tasktest, logtest: test utilities
# - docs: embedded Swaager (OpenAPI) document
# - cmd/server/{main,db,logger,pprof}.go: mostly wires libraries up and bootstraps the server
COVER_EXCLUDE := \.gen\.go|/sqlcgen/|/tasktest/|/logtest/|/docs/|cmd/server/(main|db|logger|pprof)\.go

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

.PHONY: gen-check
gen-check: gen ## Fail if generated code is out of date (run on a clean checkout)
	@test -z "$$(git status --porcelain)" || { git status --short; echo "Generated files are out of date: run make generate and commit."; exit 1; }

.PHONY: tidy-check
tidy-check: ## Fail if go.mod/go.sum are not tidy (run on a clean checkout)
	go mod tidy
	@test -z "$$(git status --porcelain go.mod go.sum)" || { git diff go.mod go.sum; exit 1; }

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
cover: ## Tests with coverage (writes coverage.out; Postgres tests need TEST_DATABASE_URL)
	go test -race -shuffle=on -count=1 -coverpkg=./... -coverprofile=$(COVER_PROFILE).raw ./...
	grep -v -E '$(COVER_EXCLUDE)' $(COVER_PROFILE).raw > $(COVER_PROFILE)
	go tool cover -func=$(COVER_PROFILE) | tail -n 1

.PHONY: cover-check
cover-check: ## Fail if total coverage is below COVER_MIN (run after `make cover`)
	@test -f $(COVER_PROFILE) || { echo "$(COVER_PROFILE) not found: run make cover first."; exit 1; }
	@total=$$(go tool cover -func=$(COVER_PROFILE) | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
	awk -v t="$$total" -v m="$(COVER_MIN)" 'BEGIN { \
		if (t+0 < m+0) { printf "coverage %.1f%% is below the floor of %s%%\n", t, m; exit 1 } \
		printf "coverage %.1f%% (floor %s%%)\n", t, m \
	}'

.PHONY: cover-html
cover-html: cover ## Write coverage.html
	go tool cover -html=$(COVER_PROFILE) -o coverage.html

.PHONY: cover-summary
cover-summary: ## Print a Markdown coverage summary (used by CI)
	@echo "### Test coverage"
	@echo
	@echo "**Total: $$(go tool cover -func=$(COVER_PROFILE) | awk '/^total:/ {print $$3}')** (floor: $(COVER_MIN)%)"
	@echo
	@echo "Lowest-covered functions:"
	@echo
	@echo "| Function | Location | Coverage |"
	@echo "|---|---|---|"
	@go tool cover -func=$(COVER_PROFILE) | grep -v '^total:' | sort -k3 -n | head -10 | awk -v mod="$(MODULE)/" -v base="$(COVER_LINK_BASE)" -f scripts/cover-rows.awk

# --- CI -----------------------------------------------------------
.PHONY: _require-test-db
_require-test-db:
	@test -n "$${TEST_DATABASE_URL:-}" || { echo "TEST_DATABASE_URL is not set: Postgres tests would be skipped."; exit 1; }

.PHONY: ci
ci: fmt-check vet lint test-db gen-check tidy-check _require-test-db cover cover-check ## Everything CI runs

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
