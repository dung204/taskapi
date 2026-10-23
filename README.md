# taskapi

A REST API for managing tasks, written in Go with the standard library (`net/http`, `log/slog`, `testing`, ...) plus a few small dependencies: the [`chi`](https://github.com/go-chi/chi) router, the [`pgx`](https://github.com/jackc/pgx) PostgreSQL driver, and the [`oapi-codegen`](https://github.com/oapi-codegen/oapi-codegen) runtime. The routes and request/response types are generated from the OpenAPI spec in `internal/httpapi/docs/openapi.yml`.

> **Status:** early development. The task endpoints, the in-memory and PostgreSQL stores, and a background worker that marks overdue tasks are implemented. There is no migration runner yet, so the database schema is applied by hand (see [PostgreSQL](#postgresql)).

## Requirements

- Go 1.27+
- PostgreSQL (only when `STORE=postgres`)

## Getting started

```sh
go run ./cmd/server
```

The server listens on `:8080` and keeps tasks in memory by default. To check that it's up:

```sh
curl http://localhost:8080/healthz
# {"status":"ok","store":"ok"}
```

Stop it with `Ctrl+C` or `SIGTERM`. The server shuts down gracefully and stops the worker.

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
| `TEST_DATABASE_URL` | (none) | Postgres connection string for the store tests. Not read by the server      |

Example:

```sh
PORT=3000 LOG_FORMAT=json go run ./cmd/server
```

### PostgreSQL

Create the schema once with the SQL in `migrations/`, then start the server with the postgres store:

```sh
psql "$DATABASE_URL" -f migrations/001_create_tasks.sql

STORE=postgres \
DATABASE_URL='postgres://user:password@localhost:5432/taskapi?sslmode=disable' \
go run ./cmd/server
```

When `STORE=postgres`, `/healthz` also pings the database.

## API

| Method   | Path          | Description                                                  |
| -------- | ------------- | ------------------------------------------------------------ |
| `GET`    | `/healthz`    | Health check. Returns `503` if the store can not be reached. |
| `POST`   | `/tasks`      | Create a task. Returns `201` with the task.                  |
| `GET`    | `/tasks`      | List tasks, newest first. Returns `[]` when there are none.  |
| `GET`    | `/tasks/{id}` | Get one task. Returns `404` if it does not exist.            |
| `PATCH`  | `/tasks/{id}` | Update some fields of a task. Returns the updated task.      |
| `DELETE` | `/tasks/{id}` | Delete a task. Returns `204`.                                |
| `GET`    | `/openapi.yaml` | The OpenAPI spec.                                          |
| `GET`    | `/docs/`      | Swagger UI for the API.                                      |

Any other method on a registered path returns `405 Method Not Allowed`. Errors are returned as `{"error": "message"}`: `400` for invalid input (including a malformed body or bad query parameter), `404` for an unknown task, `503` when the store is unavailable or times out, and `500` for anything else.

Every response carries an `X-Request-ID` header. Send your own (letters, digits and `-`, up to 64 characters) to have it reused, otherwise one is generated.

### Task

```json
{
  "id": "01a142e5-6a3e-7218-9722-c0215445fae0",
  "title": "Write the README",
  "description": "",
  "status": "todo",
  "due_at": "2030-01-01T00:00:00Z",
  "created_at": "2026-10-01T00:00:00Z",
  "updated_at": "2026-10-01T00:00:00Z"
}
```

| Field         | Rules                                                                                  |
| ------------- | -------------------------------------------------------------------------------------- |
| `title`       | Required. Surrounding whitespace is trimmed, then it must be 1-200 characters.         |
| `description` | Optional, at most 2000 characters. Defaults to `""`.                                   |
| `status`      | `todo` (default), `doing` or `done`. `overdue` is set by the worker and can not be sent. |
| `due_at`      | Optional RFC 3339 date-time, for example `2030-01-01T00:00:00Z` or `2030-01-01T10:00:00+07:00`. Stored and returned in UTC. |

`id` is a UUIDv7, which is why listing by `id` descending gives newest first. Timestamps are stored with microsecond precision.

### Updating

`PATCH /tasks/{id}` only changes the fields present in the body. A patch is applied all or nothing: if any field is invalid, nothing is changed. `updated_at` only moves when a field actually changes, so sending the current values back is a no-op.

```sh
curl -X PATCH http://localhost:8080/tasks/$ID \
  -H 'Content-Type: application/json' \
  -d '{"status":"doing"}'
```

### Listing

`GET /tasks` accepts these query parameters:

| Parameter | Default | Description                                                    |
| --------- | ------- | -------------------------------------------------------------- |
| `limit`   | `20`    | Page size, at least 1. Values above 100 are capped at 100.     |
| `offset`  | `0`     | Number of tasks to skip                                        |
| `status`  | (all)   | Only tasks with this status: `todo`, `doing`, `done` or `overdue` |

### Overdue tasks

While the worker runs, each scan sets the status of every task to `overdue` when its `due_at` has passed, unless the task is already `done` or `overdue`. Tasks without a `due_at` are never marked.

`requests.http` contains ready-to-run requests for the VS Code REST Client and JetBrains HTTP Client.

## Development

```sh
go build -buildvcs=false ./...   # build
go vet ./...
go test ./...
go test -race ./...
go generate ./...                # regenerate oapigen after editing internal/httpapi/docs/openapi.yml
```

`-buildvcs=false` avoids a VCS-stamping error from `go build` in some environments.

### Tests

The unit and handler tests run without any setup. The PostgreSQL store tests are skipped unless `TEST_DATABASE_URL` is set:

```sh
TEST_DATABASE_URL='postgres://user:password@localhost:5432/test_tasks?sslmode=disable' \
go test -race ./internal/store/postgres/
```

The `tasks` table must already exist in that database, and **every test truncates it**, so use a dedicated database.

The memory and PostgreSQL stores share one behaviour suite, `tasktest.TestStore` in `internal/task/tasktest`. Any new store should run it too.

## Project layout

```
cmd/server/            entry point: config, logger, database connection, graceful shutdown
internal/httpapi/      chi router, handlers, error mapping, middlewares (request ID, logging, panic recovery)
  docs/                OpenAPI spec (openapi.yml) and Swagger UI
  oapigen/             code generated from the spec by oapi-codegen, plus API <-> domain converters
internal/task/         task model, validation and the Store interface
  tasktest/            shared test helpers and the store behaviour suite
internal/store/
  memory/              in-memory store
  postgres/            PostgreSQL store
internal/worker/       background worker that marks overdue tasks
internal/logtest/      in-memory slog handler for asserting on log records
migrations/            SQL for the PostgreSQL schema
```
