# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

A task-management REST API in Go (module `github.com/dung204/taskapi`, Go 1.27). Routing is the `chi` router. The routes and handler glue are generated from the OpenAPI spec (`internal/httpapi/docs/openapi.yml`) by `oapi-codegen` in strict-server mode, so a wrong method on a registered path gets 405 from chi.

**Keep dependencies minimal.** Beyond the standard library (`log/slog`, UUIDs, `testing`), the runtime dependencies are the `pgx` Postgres driver, the `chi` router, the `oapi-codegen` runtime used by the generated code, `prometheus/client_golang` (metrics) and `golang.org/x/time/rate` (the token bucket in the rate limiter). `github.com/google/go-cmp` is the one test-only library, used by the `tasktest` and `logtest` helpers. Do not add other libraries (web frameworks, ORMs, logging libraries, assertion/mock libraries, config loaders, ...). If something seems to need one, write it with the standard library, or ask first.

Implemented: task CRUD, an in-memory store, a Postgres store, a background worker that marks overdue tasks, request-ID/logging/recover middlewares, optional CORS, per-IP rate limiting (429 + `Retry-After`), Prometheus metrics, an optional pprof server, an ops server (`/healthz`, `/metrics`) on its own port, and graceful shutdown. Migrations are applied with goose (`make migrate-up`, or the `migrate` service in `docker-compose.yml`).

## Commands

The `Makefile` wraps the usual Go commands (`make help` lists every target). If `go build` fails with a VCS-stamping error, add `-buildvcs=false`. Notable targets: `make ci` (what CI runs: fmt-check, vet, lint, test-db, gen-check, tidy-check, cover, cover-check; it needs `TEST_DATABASE_URL`), `make cover` (writes `coverage.out`, excluding generated code, `tasktest`, `authtest`, `emailtest`, `logtest`, `testutil`, `docs` and the bootstrap files listed in `COVER_EXCLUDE`; `make cover-check` fails below `COVER_MIN`, default 70), `make lint` / `make fmt` (golangci-lint, downloaded into `bin/` by `make tools`), `make gen`, and `make migrate-up|down|status|create`. `lefthook.yml` runs `make lint` on pre-commit and `make test` on pre-push (`make hooks` installs them). `.github/workflows/ci.yml` runs `make ci` against a Postgres service and uploads the coverage report.

`docker-compose.yml` brings up the whole stack: `db` (Postgres 17), `migrate` (goose, from the `Dockerfile`'s `migrate` target), `api` (the `server` target, healthcheck via `/server healthcheck`; only port 8080 is published on all interfaces, the ops port 9091 is not published), `prometheus` (`prometheus.yml`) and `grafana` (provisioned from `grafana/`; `GRAFANA_ADMIN_PASSWORD` must be set in `.env`). `scripts/loadtest.sh` generates mixed traffic for the dashboards and `scripts/ratelimit-demo.sh` trips the rate limiter.

The Postgres store tests are skipped unless `TEST_DATABASE_URL` is set, and **every run truncates the `tasks` table**, so it must point at a dedicated database whose schema is already applied:

```sh
TEST_DATABASE_URL='postgres://user:password@localhost:5432/test_tasks?sslmode=disable' go test -race ./internal/store/postgres/
```

`requests.http` holds manual smoke requests (REST Client format) with their expected responses written as comments.

## Configuration

`cmd/server/config.go` reads environment variables (`.env.example` documents them all; defaults live in the code). Rules the code alone does not make obvious:

- An empty value counts as unset, and every invalid value is reported together at startup.
- `DATABASE_URL` is always required and validated,.
- `RATE_LIMIT_RPS`: `0` disables rate limiting; negative, `NaN` and `Inf` are rejected. `RATE_LIMIT_BURST` is only validated when rate limiting is enabled.
- `CORS_ALLOWED_ORIGINS`: empty means the CORS middleware is not installed. Entries are bare `http(s)://host[:port]` (no path, query or credentials), normalized to lowercase without the scheme's default port and deduplicated; `*` must be the whole value (not mixed with origins, no `https://*.example.com`). `CORS_MAX_AGE` is whole seconds, `0` omits the header, and it is only validated when `CORS_ALLOWED_ORIGINS` is set.
- `APP_BASE_URL` is a bare `http(s)://host[:port]` origin, normalized like the CORS origins.
- `LOGIN_*` durations must be whole milliseconds, at least `1ms`, because the Redis scripts count in milliseconds. `LOGIN_WINDOW` must be at least the longest `LOGIN_BACKOFF` step.
- `OUTBOX_*`: `loadOutbox` requires `OUTBOX_LEASE > OUTBOX_BATCH_SIZE * OUTBOX_SEND_TIMEOUT` (so a lease cannot expire while its batch is still being sent) and `OUTBOX_RETRY_CAP >= OUTBOX_RETRY_BASE`. Each check runs only when the values involved are valid on their own.
- `SMTP_USERNAME` / `SMTP_PASSWORD`: set both or neither; not trimmed.
- `GRAFANA_ADMIN_PASSWORD` is only used by `docker-compose.yml`, and `TEST_DATABASE_URL` only by the Postgres store tests; the server never reads them.

## Architecture

- `internal/httpapi/oapigen`: `oapi.gen.go` is generated by `oapi-codegen` (`oapi-codegen.yml`: models, chi server, strict server) and **must not be edited**; regenerate with `go generate ./...`. `helpers.go` converts between API types and domain types.
- `internal/store/postgres` is the `task.Store` used by the server; `internal/store/memory` is a test-only, owner-only fake of it (no members, no assignee emails) for the `httpapi` tests. Handlers only see the interface. The Postgres store uses sqlc-generated code (`queries/`, `sqlcgen/`, `sqlc.yml`; regenerate with `make gen-sql`).
- `internal/ratelimit`: `MemoryLimiter` keeps its buckets in process memory, so limits are neither shared between instances nor kept across restarts.
- `internal/auth/authtest`: sessions and tokens have a foreign key to users, so the store suites take a factory that also returns a `UserStore`.
- `internal/testutil`: helpers shared by the `*test` packages; it must not import domain packages.
- `migrations/`: goose SQL migrations (linted by squawk via `make lint-sql`).

Flow: `main` builds the Postgres task store → passes it into `httpapi.NewHandler` → handlers call the `task.Store` interface and never a concrete backend.

Package-specific rules live in `internal/httpapi/CLAUDE.md` and `internal/task/CLAUDE.md`; Claude loads them when working in those directories.

## Documenting interfaces

Document every method of an interface where the interface is declared (for store interfaces, that is the contract, not the Postgres or memory implementation, whose methods only say `// Name implements [pkg.Interface].`). Test-only interfaces are exempt. Write the comment from the behavior of the implementations, and keep it free of anything specific to one of them (SQL, Postgres or Redis errors, locking, driver types).

- **First sentence**: say what the method does, in the third person. `Create`/`Add` methods read "Create persists a [Session]." (not "stores" or "creates a session in the database"). Say what is not checked when it matters (for example that `SetStatus` does not validate the transition).
- **Name the return values** in the interface signature (`(session Session, err error)`), and name `err` too, since Go needs all or none. A method that returns only `error` has no names and no "Returns:" section.
- **`Returns:`** comes first and lists each non-error value as `//   - name - description`. For values such as counts, say what is counted (`deleted - the number of sessions removed`).
- **`Returns the following errors:`** comes after, one bullet per error as `//   - [ErrX] - when it occurs`. List the business errors the method can return: those in `internal/task/errors.go`, `internal/auth/errors.go` and `internal/email/errors.go`, and `apperr` errors. Do not list errors the store never returns because validation happens earlier. For failures with no business error, use a generic bullet (`an error - if the backend is unavailable`) only where callers need to know (limiters, revokers, `WithTx`).
- **No-ops and idempotence**: when a method does nothing for an unknown ID, or deleting nothing is fine, say so instead of listing an error.
- **Known gaps**: if an implementation does not yet match the contract, document the contract and add "(not yet reported by the Postgres store, ...)" to the bullet, so the gap is visible until it is fixed.

Example:

```go
// GetByRefreshTokenHash fetches the [Session] whose current or previous
// refresh token hash equals hash. It does not check whether the session has
// expired.
//
// Returns:
//   - session - the matching [Session].
//
// Returns the following errors:
//   - [ErrSessionNotFound] - if no session matches.
GetByRefreshTokenHash(ctx context.Context, hash []byte) (session Session, err error)
```
