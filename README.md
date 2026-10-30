# taskapi

![Go](https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white)
![chi](https://img.shields.io/badge/router-chi-2E7D32)
![OpenAPI](https://img.shields.io/badge/OpenAPI-oapi--codegen-6BA539?logo=openapiinitiative&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white)
![sqlc](https://img.shields.io/badge/sqlc-pgx-3B82F6)
![goose](https://img.shields.io/badge/migrations-goose-8A4FFF)
![Prometheus](https://img.shields.io/badge/Prometheus-metrics-E6522C?logo=prometheus&logoColor=white)
![Grafana](https://img.shields.io/badge/Grafana-dashboards-F46800?logo=grafana&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)
![GitHub Actions](https://img.shields.io/badge/CI-GitHub%20Actions-2088FF?logo=githubactions&logoColor=white)

A REST API for managing tasks, written in Go with the standard library (`net/http`, `log/slog`, `testing`, ...) plus a few small dependencies: the [`chi`](https://github.com/go-chi/chi) router, the [`pgx`](https://github.com/jackc/pgx) PostgreSQL driver, the [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) runtime, [`prometheus/client_golang`](https://github.com/prometheus/client_golang) for metrics and [`golang.org/x/time/rate`](https://pkg.go.dev/golang.org/x/time/rate) for rate limiting. The routes and request/response types are generated from the OpenAPI spec in `internal/httpapi/docs/openapi.yml`.

> **Status:** early development. Implemented: the task endpoints, the in-memory and PostgreSQL stores, a background worker that marks overdue tasks, per-IP rate limiting, Prometheus metrics, an optional pprof server, and a docker compose stack with Prometheus and Grafana. Migrations are applied with [goose](https://github.com/pressly/goose) (see [PostgreSQL](#postgresql)).

## Requirements

- Go 1.27+
- PostgreSQL (only when `STORE=postgres`)
- Docker with Compose (only for the full stack)

## Getting started

```sh
go run ./cmd/server
```

The server listens on `:8080` and keeps tasks in memory by default. To check that it's up:

```sh
curl http://localhost:9091/healthz
# {"status":"ok","store":"ok"}
```

Stop it with `Ctrl+C` or `SIGTERM`. The server shuts down gracefully and stops the worker.

### Docker compose

`docker compose up --build` starts the whole stack: Postgres, the goose migrations, the API (with `STORE=postgres`), Prometheus and Grafana. Set `GRAFANA_ADMIN_PASSWORD` in `.env` first (copy `.env.example`). Grafana is at http://localhost:3000 with the `taskapi` dashboard provisioned. `scripts/loadtest.sh` generates mixed traffic for the dashboards and `scripts/ratelimit-demo.sh` trips the rate limiter.

## Ports

| Port   | Service    | Configured by                          | Exposed on host (docker compose) | Serves                                                         |
| ------ | ---------- | -------------------------------------- | -------------------------------- | -------------------------------------------------------------- |
| `8080` | API        | `PORT`                                 | `0.0.0.0:8080`                   | Task API, `/docs/` (Swagger UI), `/openapi.yaml`               |
| `9091` | Ops        | `OPS_ADDR` (default `127.0.0.1:9091`)  | not published                    | `GET /healthz`, `GET /metrics` (scraped by Prometheus)         |
| `6060` | pprof      | `PPROF_ADDR` (unset = disabled)        | `127.0.0.1:6060`                 | `/debug/pprof/`                                                |
| `5432` | PostgreSQL | `POSTGRES_*` in `.env`                 | `127.0.0.1:5432`                 | Database (`db` service)                                        |
| `9090` | Prometheus | `docker-compose.yml`                       | `127.0.0.1:9090`                 | Prometheus UI                                                  |
| `3000` | Grafana    | `GRAFANA_ADMIN_PASSWORD` in `.env`     | `127.0.0.1:3000`                 | Grafana dashboards                                             |

The ops and pprof servers listen on their own ports, separate from the API port. Only the API port is meant to be reachable from outside. Outside docker compose, the API, ops and pprof ports are the ones the server itself opens.

## Configuration

Settings come from environment variables (see `.env.example`). An empty value counts as unset. An invalid value stops the server at startup with an error that lists every problem.

| Variable          | Default  | Description                                                                 |
| ----------------- | -------- | --------------------------------------------------------------------------- |
| `PORT`            | `8080`   | Port the HTTP server listens on (1-65535)                                   |
| `STORE`           | `memory` | Storage backend: `memory` or `postgres`                                     |
| `DATABASE_URL`    | (none)   | Postgres connection string. Required when `STORE=postgres`                  |
| `LOG_FORMAT`      | `text`   | Log format: `text` or `json`                                                |
| `LOG_LEVEL`       | `info`   | Log level: `debug`, `info`, `warn` or `error`                               |
| `WORKER_ENABLED`  | `true`   | Run the background worker that marks overdue tasks                          |
| `WORKER_INTERVAL` | `30s`    | How often the worker scans, as a positive duration such as `500ms`, `30s` or `1m` |
| `OPS_ADDR`        | `127.0.0.1:9091` | Listen address of the ops server (`/healthz`, `/metrics`), such as `:9091` |
| `PPROF_ADDR`      | (none)   | Listen address of the pprof server, such as `127.0.0.1:6060`. Unset means no pprof server |
| `RATE_LIMIT_RPS`  | `10`     | Requests per second allowed for each client (float such as `0.5`). `0` disables rate limiting |
| `RATE_LIMIT_BURST` | `20`    | Bucket size for each client, an integer of at least 1. Only checked when rate limiting is on |
| `CORS_ALLOWED_ORIGINS` | (none) | Origins allowed to call the API from a browser, comma-separated, such as `https://app.example.com,http://localhost:5173`. `*` allows any origin. Empty disables CORS |
| `CORS_MAX_AGE`    | `10m`    | How long browsers may cache a CORS preflight, a non-negative duration in whole seconds. Only checked when `CORS_ALLOWED_ORIGINS` is set. `0` omits the header |
| `GRAFANA_ADMIN_PASSWORD` | (none) | Grafana admin password. Only used by `docker-compose.yml`, never by the server |
| `TEST_DATABASE_URL` | (none) | Postgres connection string for the store tests. Not read by the server      |

Example:

```sh
PORT=3000 LOG_FORMAT=json go run ./cmd/server
```

### Why `OPS_ADDR` differs in docker compose

The ops server exposes metrics and health details with no authentication, so by default it binds to loopback only (`127.0.0.1:9091`) and is reachable just from the machine the server runs on.

Inside a container, `127.0.0.1` is the container's own loopback, so Prometheus (a different container) could not scrape `api:9091`. `docker-compose.yml` therefore sets `OPS_ADDR=:9091` to listen on every interface of the container. This stays private because the compose file does not publish port 9091 to the host: only containers on the compose network can reach it. The `/server healthcheck` subcommand runs inside the container and treats an address without a host as `127.0.0.1`, so it works with both values.

If you set `OPS_ADDR` to a non-loopback address outside docker, put it behind a firewall or private network.

### PostgreSQL

The schema lives in `migrations/` as goose migrations. Apply them with `make migrate-up` (it needs `DATABASE_URL`; `make migrate-down`, `migrate-status` and `migrate-create name=...` are also available), then start the server with the postgres store:

```sh
make migrate-up

STORE=postgres \
DATABASE_URL='postgres://user:password@localhost:5432/taskapi?sslmode=disable' \
go run ./cmd/server
```

When `STORE=postgres`, `/healthz` (on the ops server) also pings the database.

## API

The API is documented by its OpenAPI spec. While the server is running, see the Swagger UI at http://localhost:8080/docs/ for every endpoint, schema, status code and example, or fetch the raw spec from http://localhost:8080/openapi.yaml. Both are on the API port and are not rate limited.

### Ops server

Health and metrics are served on the separate ops port (see [Ports](#ports)), not on the API port:

| Method | Path       | Description                                                                 |
| ------ | ---------- | --------------------------------------------------------------------------- |
| `GET`  | `/healthz` | Health check. Returns `503` if the store can not be reached.                |
| `GET`  | `/metrics` | Prometheus metrics: Go and process stats, `http_*`, `db_pool_*` and `rate_limit_*`. |

The server binary also has a `healthcheck` subcommand (`/server healthcheck`) that calls `/healthz` on `OPS_ADDR`. The compose healthcheck uses it.

### Rate limiting

Each client IP gets a token bucket of `RATE_LIMIT_BURST` requests, refilled at `RATE_LIMIT_RPS` per second (IPv6 addresses are grouped by their /64). A client over the limit gets `429` with a `Retry-After` header. `/docs/` and `/openapi.yaml` are not limited. The limiter lives in process memory, so limits are not shared between instances or kept across restarts. Clients are keyed by the connection's address only, so behind a proxy or a Docker network all clients share one bucket (`X-Forwarded-For` is ignored).

### CORS

CORS is off unless `CORS_ALLOWED_ORIGINS` is set, so browsers only allow same-origin requests. When it is set:

- Each entry must be a bare `scheme://host[:port]` origin (`http` or `https`), with no path, trailing slash, query or credentials. Entries are lowercased, a default port (`:80` on http, `:443` on https) is dropped because browsers never send it, and duplicates are removed.
- `*` allows any origin, but it must be the whole value: it can not be combined with other origins or used for subdomains such as `https://*.example.com`.
- A preflight (`OPTIONS` with `Origin` and `Access-Control-Request-Method`) from an allowed origin gets `204` with `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods` (`GET, POST, PATCH, DELETE`), `Access-Control-Allow-Headers` (`Content-Type, X-Request-ID, Authorization`) and, unless `CORS_MAX_AGE=0`, `Access-Control-Max-Age`. From any other origin it still gets `204` but without these headers, so the browser blocks the request.
- Other requests from an allowed origin get `Access-Control-Allow-Origin` and `Access-Control-Expose-Headers` (`X-Request-ID, Retry-After`), so a page can read the request ID and the `Retry-After` of a `429`. The allowed origin is echoed back (also for `*`) with `Vary: Origin`. Credentials (cookies) are not allowed.
- CORS runs before the request ID, metrics, logging and rate limiting middlewares, so preflights are answered without being logged, counted or limited. It also applies to `/docs/` and `/openapi.yaml`.

### Request IDs

Every response carries an `X-Request-ID` header. Send your own (letters, digits and `-`, up to 64 characters) to have it reused, otherwise one is generated.

### Overdue tasks

While the worker runs, each scan sets the status of every task to `overdue` when its `due_at` has passed, unless the task is already `done` or `overdue`. Tasks without a `due_at` are never marked.

## Development

Everything goes through the `Makefile` (`make help` lists every target). `make` reads `.env` if present.

```sh
make run             # run the server
make build           # build the server into bin/server
make test            # go test -race -shuffle=on ./...
make vet             # go vet
make lint            # golangci-lint (downloaded into bin/ on first use)
make fmt             # format the code
make gen             # go generate: regenerate oapigen after editing internal/httpapi/docs/openapi.yml
make gen-sql         # regenerate the sqlc code after editing the SQL queries
make tidy            # go mod tidy
make cover           # tests with coverage, writes coverage.out (make cover-html for a report)
make hooks           # install the lefthook git hooks (lint on pre-commit, tests on pre-push)
make ci              # everything CI runs (needs TEST_DATABASE_URL)
```

Migrations: `make migrate-up`, `migrate-down`, `migrate-status` and `migrate-create name=add_index` (they need `DATABASE_URL`). `make lint-sql` lints them with squawk (`make tools` installs the binary tools).

If `go build` fails with a VCS-stamping error in your environment, build with `go build -buildvcs=false ./...`.

### Tests

The unit and handler tests run without any setup. The PostgreSQL store tests are skipped unless `TEST_DATABASE_URL` is set:

```sh
TEST_DATABASE_URL='postgres://user:password@localhost:5432/test_tasks?sslmode=disable' make test-db
```

The `tasks` table must already exist in that database, and **every test truncates it**, so use a dedicated database.

The memory and PostgreSQL stores share one behaviour suite, `tasktest.TestStore` in `internal/task/tasktest`. Any new store should run it too.

## Project layout

```
.
├── cmd/server/            entry point: config, logger, database connection, ops and pprof servers, graceful shutdown
├── internal/
│   ├── httpapi/           chi router, handlers, error mapping, middlewares (request ID, metrics, logging, panic recovery, rate limiting)
│   │   ├── docs/          OpenAPI spec (openapi.yml) and Swagger UI
│   │   └── oapigen/       code generated from the spec by oapi-codegen, plus API <-> domain converters
│   ├── task/              task model, validation and the Store interface
│   │   └── tasktest/      shared test helpers and the store behaviour suite
│   ├── store/
│   │   ├── memory/        in-memory store
│   │   └── postgres/      PostgreSQL store (sqlc-generated queries, pool metrics)
│   ├── ratelimit/         in-memory per-key token bucket limiter
│   ├── worker/            background worker that marks overdue tasks
│   └── logtest/           in-memory slog handler for asserting on log records
├── migrations/            goose SQL migrations for the PostgreSQL schema
├── grafana/               provisioned Grafana datasource and dashboards
├── scripts/               load test and rate limit demo scripts
├── Dockerfile             `server` and `migrate` images
├── docker-compose.yml     db, migrate, api, prometheus and grafana
└── prometheus.yml         Prometheus scrape config
```
