#!/usr/bin/env bash
# Runs the Go tests against a migrated template database.
#
#   scripts/test.sh [go test args...]     (default: -race -shuffle=on ./...)
#
# With TEST_DATABASE_URL set, that database is the template (CI, or your own
# Postgres) and Docker is not used. Otherwise a throwaway Postgres is started
# from docker-compose.test.yml as the separate project "taskapi-test" and left
# running between runs (remove it with `make test-down`).
#
# The template is only migrated here; each test package clones its own database
# from it (internal/testutil), so never run tests directly on it.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ -z "${TEST_DATABASE_URL:-}" ]; then
	if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
		echo "error: Docker is not available, so the test Postgres cannot be started." >&2
		echo "Start Docker, or set TEST_DATABASE_URL to a migrated, dedicated test database" >&2
		echo "(its name must contain \"test\"), e.g. postgres://user:pass@localhost:5432/test_tasks?sslmode=disable" >&2
		exit 1
	fi
	docker compose -p taskapi-test -f docker-compose.test.yml up -d --wait
	export TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:55432/taskapi_test_template?sslmode=disable'
fi

case "$(basename "${TEST_DATABASE_URL%%\?*}")" in
*test*) ;;
*)
	echo "error: the database name in TEST_DATABASE_URL must contain \"test\"." >&2
	exit 1
	;;
esac

go tool goose -dir migrations postgres "$TEST_DATABASE_URL" up

if [ "$#" -eq 0 ]; then
	set -- -race -shuffle=on ./...
fi
exec go test "$@"
