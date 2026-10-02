# taskapi

A REST API for managing tasks, written in Go using only the standard library.

> **Status:** early development. Only the health check endpoint is implemented so far. Task endpoints, storage backends and database migrations are scaffolded but not built yet.

## Requirements

- Go 1.27+
- PostgreSQL (optional, only for the planned `postgres` store)

## Getting started

```sh
go run ./cmd/server
```

The server listens on `:8080` by default. To check that it's up:

```sh
curl http://localhost:8080/healthz
# {"status":"ok"}
```

## Configuration

Settings come from environment variables. An empty value counts as unset.

| Variable       | Default  | Description                                      |
| -------------- | -------- | ------------------------------------------------ |
| `PORT`         | `8080`   | Port the HTTP server listens on                  |
| `STORE`        | `memory` | Storage backend: `memory` or `postgres` (planned) |
| `DATABASE_URL` | (none)   | Postgres connection string, used by the `postgres` store |

Example:

```sh
PORT=3000 go run ./cmd/server
```

## API

| Method | Path       | Description                                  |
| ------ | ---------- | -------------------------------------------- |
| `GET`  | `/healthz` | Health check. Returns `{"status":"ok"}`.     |

Any other method on a registered path returns `405 Method Not Allowed`.

`requests.http` contains ready-to-run requests for the VS Code REST Client and JetBrains HTTP Client.

## Development

```sh
go build -buildvcs=false ./...   # build (see note below)
go vet ./...
go test ./...
```

`-buildvcs=false` is needed until the directory is a git repository. Without it, `go build` fails with "error obtaining VCS status".

## Project layout

```
cmd/server/          entry point and environment config
internal/httpapi/    HTTP routes, handlers and JSON response helpers
internal/task/       task domain model (planned)
internal/store/      storage interface (planned)
  memory/            in-memory implementation (planned)
  postgres/          PostgreSQL implementation (planned)
migrations/          SQL migrations for PostgreSQL
```
