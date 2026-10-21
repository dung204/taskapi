# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

A task-management REST API in Go (module `github.com/dung204/taskapi`, Go 1.27). Routing is `net/http`'s `ServeMux` with method-qualified patterns (`"GET /healthz"`), so a wrong method automatically gets 405.

**This version is plain Go.** The only external dependency is the `pgx` Postgres driver, used through `database/sql`; everything else is the standard library (routing, `log/slog`, UUIDs, `testing`). Do not add other libraries (routers, web frameworks, ORMs or query builders, logging libraries, assertion/mock libraries, config loaders, migration tools, ...). If something seems to need one, write it with the standard library, or ask first. A separate version that integrates libraries is planned (this source will be split into a plain-Go version and a library-based one), so keep this one free of any dependency besides `pgx`.

Implemented: task CRUD, an in-memory store, a Postgres store, a background worker that marks overdue tasks, request-ID/logging/recover middlewares, and graceful shutdown. Not implemented: a migration runner.

## Commands

```sh
go run ./cmd/server                      # start the server (default :8080)
go build ./...                           # build (if it fails with a VCS-stamping error, add -buildvcs=false)
go vet ./...
go test -race ./...
go test ./internal/httpapi -run TestName # run a single test
```

The Postgres store tests are skipped unless `TEST_DATABASE_URL` is set, and **every run truncates the `tasks` table**, so it must point at a dedicated database whose schema is already applied:

```sh
TEST_DATABASE_URL='postgres://user:password@localhost:5432/test_tasks?sslmode=disable' go test -race ./internal/store/postgres/
```

`requests.http` holds manual smoke requests (REST Client format) with their expected responses written as comments.

## Configuration

`cmd/server/env.go` reads environment variables. An empty value counts as unset, and every invalid value is reported together at startup. `.env.example` documents them all.

- `PORT`: listen port, default `8080`
- `STORE`: `memory` (default) or `postgres`
- `DATABASE_URL`: Postgres DSN, required when `STORE=postgres`
- `LOG_FORMAT`: `text` (default) or `json`
- `LOG_LEVEL`: `debug` | `info` | `warn` | `error`, default `info`
- `WORKER_ENABLED`: default `true`
- `WORKER_INTERVAL`: Go duration, default `30s`
- `TEST_DATABASE_URL`: only used by the Postgres store tests, never by the server

## Architecture

- `cmd/server`: entry point. `main.go` loads the config, builds the logger (`logger.go`), picks the store by `STORE` (the Postgres one connects through `db.go`), builds the handler with `httpapi.NewHandler(store, logger, pinger)`, starts the worker if enabled, and runs an `http.Server` with explicit timeouts. SIGINT/SIGTERM trigger a graceful shutdown that also stops the worker.
- `internal/httpapi`: the HTTP layer. `routes.go` registers the routes and wraps the mux in the middlewares (request ID → logging → panic recovery, in `middlewares.go`). `task.go` holds the task handlers and `handleStoreError`, which maps store errors to responses. `health.go` serves `/healthz` and pings the store when it has a pinger (the Postgres `*sql.DB`). `helpers.go` has `writeJSON` / `writeError` / `readJSON` / `parseTime`.
- `internal/task`: the domain. `task.go` has `Task`, `New` and `Patch`, which own all validation. `store.go` has the `Store` interface plus `ListFilter` and `Patch`. `errors.go` has the sentinel errors (`ErrNotFound`, `ErrInvalidInput`, `ErrServiceUnavailable`, ...).
- `internal/store/memory` and `internal/store/postgres`: the two `task.Store` implementations. Handlers only see the interface.
- `internal/worker`: `Run` ticks every `WORKER_INTERVAL` and calls `Store.MarkOverdue`. A panic in a scan is recovered and logged, and the loop keeps going.
- `internal/task/tasktest`: shared test code: `CheckTask` / `CheckPatch` and `TestStore`, the behaviour suite that both stores run.
- `internal/logtest`: an in-memory `slog.Handler` (`logtest.New()`) for asserting on log records.
- `migrations/`: numbered SQL files for the Postgres schema. They are applied by hand with `psql`.

Flow: `main` picks a store from `STORE` → passes it into `httpapi.NewHandler` → handlers call the `task.Store` interface and never a concrete backend.

## Conventions and gotchas

- **Time precision**: timestamps are normalized to UTC and truncated to microseconds (what Postgres keeps) in `task.New` / `Task.Patch`. Tests compare times exactly, so build expected values the same way.
- **IDs and ordering**: IDs are UUIDv7. `List` returns tasks ordered by `id` DESC (newest first) in both stores, and must return a non-nil empty slice so the API responds `[]`, not `null`.
- **`Task.Patch` is atomic**: it works on a copy and only commits when every field is valid. `updated_at` moves only when a field really changes.
- **`overdue` is system-set**: it is valid for filtering (`GET /tasks?status=overdue`) but rejected in a patch or create. Only `MarkOverdue` sets it, and it skips `done` tasks and tasks without `due_at`.
- **Every store test goes in `tasktest.TestStore`** so memory and Postgres stay in sync.
- **Test style**: table-driven, using the `tasktest` checkers. To skip a field in `CheckTask`, set that field equal in `got` and `want` before calling it, rather than adding a flag.
- **`handleStoreError`** has deliberate asymmetries: `DeadlineExceeded` returns 503 without logging, `ErrServiceUnavailable` returns 503 and logs, and `context.Canceled` writes nothing (the access log then shows 200).

## Known issues in existing code

- `Update` in `internal/httpapi/task.go` and `writeJSON` in `internal/httpapi/helpers.go` still print errors to stderr instead of using the logger (both marked `TODO`).
- There is no migration runner; the schema must be applied by hand.
