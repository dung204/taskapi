# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

A task-management REST API in Go (module `github.com/dung204/taskapi`, Go 1.27). It uses only the standard library so far: routing is `net/http`'s `ServeMux` with method-qualified patterns (`"GET /healthz"`), so a wrong method automatically gets 405.

The project is at an early stage. Most of the planned layout is scaffolded but empty (see Architecture).

## Commands

```sh
go run ./cmd/server                      # start the server (default :8080)
go build -buildvcs=false ./...           # build; plain `go build` currently fails with a VCS-stamping error
go vet ./...
go test ./...                            # no tests exist yet
go test ./internal/httpapi -run TestName # run a single test
```

`requests.http` holds manual smoke requests (REST Client format) with their expected responses written as comments.

## Configuration

`cmd/server/env.go` reads environment variables. An empty value counts as unset:

- `PORT`: listen port, default `8080`
- `STORE`: storage backend, default `memory` (intended values: `memory` | `postgres`)
- `DATABASE_URL`: Postgres DSN, used only by the postgres store

`STORE` and `DATABASE_URL` are loaded but not used yet.

## Architecture

- `cmd/server`: entry point. Loads the config, builds the handler from `httpapi.NewHandler()`, and runs an `http.Server` with explicit timeouts.
- `internal/httpapi`: the HTTP layer. `routes.go` registers all routes in `NewHandler`, and dependencies such as the store and logger are meant to be passed in as parameters there (marked TODO). Handlers write responses through `writeJSON` / `writeError` in `helpers.go`.
- `internal/task`: domain model (empty).
- `internal/store`: storage abstraction (empty). Two implementations are planned: `store/memory` and `store/postgres`, chosen at startup by `STORE`.
- `migrations/`: numbered SQL files for the Postgres store. `001_create_tasks.sql` is still empty, and there is no migration runner yet.

Intended flow: `main` picks a store implementation based on `STORE` → passes it into `httpapi.NewHandler` → handlers call the store interface and never call a concrete backend directly.

## Known issues in existing code

- `writeError` in `internal/httpapi/helpers.go` has its `err != nil` check inverted, so on success it writes nothing. It also misspells the content type as `"applicaion/json"`.
- `main` ignores the error returned by `server.ListenAndServe()`.
