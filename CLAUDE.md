# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

A task-management REST API in Go (module `github.com/dung204/taskapi`, Go 1.27). Routing is the `chi` router. The routes and handler glue are generated from the OpenAPI spec (`internal/httpapi/docs/openapi.yml`) by `oapi-codegen` in strict-server mode, so a wrong method on a registered path gets 405 from chi.

**Keep dependencies minimal.** Beyond the standard library (`log/slog`, UUIDs, `testing`), the runtime dependencies are the `pgx` Postgres driver, the `chi` router, the `oapi-codegen` runtime used by the generated code, `prometheus/client_golang` (metrics) and `golang.org/x/time/rate` (the token bucket in the rate limiter). `github.com/google/go-cmp` is the one test-only library, used by the `tasktest` and `logtest` helpers. Do not add other libraries (web frameworks, ORMs, logging libraries, assertion/mock libraries, config loaders, ...). If something seems to need one, write it with the standard library, or ask first.

Implemented: task CRUD, an in-memory store, a Postgres store, a background worker that marks overdue tasks, request-ID/logging/recover middlewares, optional CORS, per-IP rate limiting (429 + `Retry-After`), Prometheus metrics, an optional pprof server, an ops server (`/healthz`, `/metrics`) on its own port, and graceful shutdown. Migrations are applied with goose (`make migrate-up`, or the `migrate` service in `docker-compose.yml`).

## Commands

```sh
go run ./cmd/server                      # start the server (default :8080)
go build ./...                           # build (if it fails with a VCS-stamping error, add -buildvcs=false)
go vet ./...
go test -race ./...
go test ./internal/httpapi -run TestName # run a single test
go test -bench . ./internal/store/memory # store benchmarks (tasktest.BenchmarkStore)
```

The `Makefile` wraps these (`make help` lists every target). Notable ones: `make ci` (what CI runs: fmt-check, vet, lint, test-db, gen-check, tidy-check, cover, cover-check; it needs `TEST_DATABASE_URL`), `make cover` (writes `coverage.out`, excluding generated code, `tasktest`, `logtest`, `docs` and the bootstrap files listed in `COVER_EXCLUDE`; `make cover-check` fails below `COVER_MIN`, default 70), `make lint` / `make fmt` (golangci-lint, downloaded into `bin/` by `make tools`), `make gen`, and `make migrate-up|down|status|create`. `lefthook.yml` runs `make lint` on pre-commit and `make test` on pre-push (`make hooks` installs them). `.github/workflows/ci.yml` runs `make ci` against a Postgres service and uploads the coverage report.

`docker-compose.yml` brings up the whole stack: `db` (Postgres 17), `migrate` (goose, from the `Dockerfile`'s `migrate` target), `api` (the `server` target, `STORE=postgres`, healthcheck via `/server healthcheck`; only port 8080 is published on all interfaces, the ops port 9091 is not published), `prometheus` (`prometheus.yml`) and `grafana` (provisioned from `grafana/`; `GRAFANA_ADMIN_PASSWORD` must be set in `.env`). `scripts/loadtest.sh` generates mixed traffic for the dashboards and `scripts/ratelimit-demo.sh` trips the rate limiter.

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
- `PPROF_ADDR`: listen address (`:6060`, `127.0.0.1:6060`) for the pprof server; unset means no pprof server
- `OPS_ADDR`: listen address (`:9091`, `127.0.0.1:9091`) of the ops server, which serves `GET /healthz` and `GET /metrics`; default `127.0.0.1:9091`, always started
- `RATE_LIMIT_RPS`: allowed requests per second per client (float, finite, default `10`); `0` disables rate limiting; negative, `NaN` and `Inf` are rejected
- `RATE_LIMIT_BURST`: bucket size per client (integer ≥ 1, default `20`); only validated when rate limiting is enabled
- `CORS_ALLOWED_ORIGINS`: comma-separated list of allowed browser origins; empty (default) means the CORS middleware is not installed. Entries are bare `http(s)://host[:port]` (no path, query or credentials), normalized to lowercase without the scheme's default port and deduplicated; `*` must be the whole value (not mixed with origins, no `https://*.example.com`)
- `CORS_MAX_AGE`: Go duration in whole seconds, non-negative, default `10m`, sent as `Access-Control-Max-Age` (`0` omits it); only validated when `CORS_ALLOWED_ORIGINS` is set
- `GRAFANA_ADMIN_PASSWORD`: only used by `docker-compose.yml`, never by the server
- `TEST_DATABASE_URL`: only used by the Postgres store tests, never by the server

## Architecture

- `cmd/server`: entry point. `main.go` first handles the `healthcheck` subcommand (`/server healthcheck`, used by the compose healthcheck), then loads the config, builds the logger (`logger.go`), picks the store by `STORE` (the Postgres one connects through `db.go`), builds the handler with `httpapi.NewHandler(store, logger, metrics, limiter, corsOpts)` (`metrics` and `limiter` may be nil; CORS is installed only when `corsOpts.AllowedOrigins` is not empty), starts the worker and the rate limiter's cleanup loop if enabled, and runs an `http.Server` with explicit timeouts. `diagnostics.go` builds the Prometheus registry (Go and process collectors, plus the HTTP, pool and limiter metrics) and the metrics handler. `internal_server.go` starts the optional pprof server and the ops server (`/healthz`, `/metrics`) on their own listeners, separate from the API port. `health.go` has the `/healthz` handler, which pings the store when `main` passes a pinger (the Postgres pool), and the `healthcheck` subcommand, which calls `/healthz` on `OPS_ADDR`. SIGINT/SIGTERM trigger a graceful shutdown that also stops the worker and the limiter loop.
- `internal/httpapi`: the HTTP layer. `routes.go` builds the chi router with the middlewares (CORS, if configured → request ID → metrics, if given → logging → panic recovery, in `middlewares.go`), mounts the generated strict handler, and serves `/openapi.yaml` and the Swagger UI at `/docs/`. It also holds the error handlers: `handleParamError` (bad path/query params → 400), `handleRequestError` (bad JSON body → 400) and `handleResponseError`, which maps errors returned by handlers to responses. `task.go` implements the generated `oapigen.StrictServerInterface`: handlers take a typed request object and return a typed response (`oapigen.CreateTask201JSONResponse`, ...) or an error. `helpers.go` has `writeJSON` / `writeError`. `metrics.go` has `Metrics` (`http_requests_total`, `http_request_duration_seconds`, `http_requests_in_flight`, labelled by method, chi route pattern and status). `cors.go` has `CORSMiddlewareOpts` and `corsMiddleware`, which answers preflights itself and adds `Access-Control-Allow-Origin` / `Expose-Headers` to responses for allowed origins and `Vary: Origin` to every response. `limiter.go` has the `Limiter` interface, the `byIP` key function (IPv6 addresses are masked to their /64) and the `rateLimit` middleware, which answers 429 with a `Retry-After` header.
- `internal/httpapi/docs`: the embedded OpenAPI spec (`openapi.yml`, the source of truth for the API) and Swagger UI.
- `internal/httpapi/oapigen`: `oapi.gen.go` is generated by `oapi-codegen` (`oapi-codegen.yml`: models, chi server, strict server) and must not be edited; regenerate with `go generate ./...`. `helpers.go` converts between API types and domain types (`ToNewParams`, `ToPatch`, `ToListFilter`, `ToAPITask`).
- `internal/task`: the domain. `task.go` has `Task`, `New` and `Patch`, which own all validation. `store.go` has the `Store` interface plus `ListFilter` and `Patch`. `errors.go` has the sentinel errors (`ErrNotFound`, `ErrInvalidInput`, `ErrServiceUnavailable`, ...).
- `internal/store/memory` and `internal/store/postgres`: the two `task.Store` implementations. Handlers only see the interface. The Postgres store uses sqlc-generated code (`queries/`, `sqlcgen/`, `sqlc.yml`; regenerate with `make gen-sql`), and `metrics.go` there has `PoolCollector`, which exports the pgx pool stats as `db_pool_*` metrics.
- `internal/ratelimit`: `MemoryLimiter`, a per-key token bucket (`x/time/rate`) kept in process memory, so limits are neither shared between instances nor kept across restarts. `Run` evicts idle keys (idle for `max(5m, burst/rps)`, so a slow-refilling bucket is not evicted before it is full) and `Len` feeds the `rate_limit_tracked_clients` gauge.
- `internal/worker`: `Run` ticks every `WORKER_INTERVAL` and calls `Store.MarkOverdue`. A panic in a scan is recovered and logged, and the loop keeps going.
- `internal/task/tasktest`: shared test code: `CheckTask` / `CheckPatch` and `TestStore`, the behaviour suite that both stores run, plus `BenchmarkStore`, the benchmark both stores share.
- `internal/logtest`: an in-memory `slog.Handler` (`logtest.New()`) for asserting on log records.
- `migrations/`: goose SQL migrations for the Postgres schema (linted by squawk via `make lint-sql`).

Flow: `main` picks a store from `STORE` → passes it into `httpapi.NewHandler` → handlers call the `task.Store` interface and never a concrete backend.

## Conventions and gotchas

- **Time precision**: timestamps are normalized to UTC and truncated to microseconds (what Postgres keeps) in `task.New` / `Task.Patch`. Tests compare times exactly, so build expected values the same way.
- **IDs and ordering**: IDs are UUIDv7. `List` returns tasks ordered by `id` DESC (newest first) in both stores, and must return a non-nil empty slice so the API responds `[]`, not `null`.
- **`Task.Patch` is atomic**: it works on a copy and only commits when every field is valid. `updated_at` moves only when a field really changes.
- **`overdue` is system-set**: it is valid for filtering (`GET /tasks?status=overdue`) but rejected in a patch or create. Only `MarkOverdue` sets it, and it skips `done` tasks and tasks without `due_at`.
- **Every store test goes in `tasktest.TestStore`** so memory and Postgres stay in sync.
- **Test style**: table-driven, using the `tasktest` checkers. `CheckTask` / `CheckPatch` take `go-cmp` options, so to skip a field pass `cmpopts.IgnoreFields(task.Task{}, "UpdatedAt")` rather than adding a flag or equalizing the values by hand.
- **Rate limiting is per route, not in `r.Use`**: it is passed as `oapigen.ChiServerOptions.Middlewares`, because `r.Use` middlewares run before chi matches a route and a rejected request would reach the metrics with the route label `unknown`. `/docs` and `/openapi.yaml` are therefore not limited. A new response status needs a `429` entry in `openapi.yml` for every operation.
- **CORS is the outermost middleware**: it must stay first in `r.Use` so preflights are answered before the router (they would be 405), the rate limiter and the logging, and so every response, including `429` and recovered panics, carries the CORS headers. The cost is that preflights are not logged or counted in metrics. Set response headers before `WriteHeader`, or they are never sent. The `Authorization` entry in the allowed request headers is a placeholder for future auth (see the `TODO` in `cors.go`).
- **Wrapping `http.ResponseWriter`**: wrappers like `statusRecorder` must keep an `Unwrap() http.ResponseWriter` method so `http.ResponseController` still reaches the real writer.
- **API changes start in `docs/openapi.yml`**: edit the spec, run `go generate ./...`, then implement the new interface methods. Handlers return expected errors (`ErrNotFound`, `ErrInvalidInput`) as the typed `...400/404JSONResponse` and everything else as an `error` for `handleResponseError`.
- **`handleResponseError`** has deliberate asymmetries: `DeadlineExceeded` returns 503 without logging, `ErrServiceUnavailable` returns 503 and logs, and `context.Canceled` writes nothing (the access log then shows 200). Converters in `oapigen/helpers.go` must wrap `task.ErrInvalidInput` so these are reported as 400.

## Known issues in existing code

- `writeJSON` in `internal/httpapi/helpers.go` still prints errors to stderr instead of using the logger (marked `TODO`).
- The rate limiter keys clients by `RemoteAddr` only (`byIP`), so behind a proxy or Docker network every client shares one bucket; `X-Forwarded-For` is not honoured.
