package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/migrations"
)

// TestMain migrates the test database once, before any test runs. A failed
// migration aborts the whole package: tests must never run against a schema
// that is missing or half applied. Without TEST_DATABASE_URL it does nothing
// and the database tests skip themselves.
func TestMain(m *testing.M) {
	if url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL")); url != "" {
		if err := migrateTestDB(url); err != nil {
			fmt.Fprintf(os.Stderr, "postgres tests: migrate test database: %v\n", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

func migrateTestDB(url string) error {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer pool.Close()

	provider, err := goose.NewProvider(
		goose.DialectPostgres, stdlib.OpenDBFromPool(pool), migrations.FS,
	)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

func truncateTable(tb testing.TB, pool *pgxpool.Pool, tableName string) {
	tb.Helper()

	_, err := pool.Exec(
		tb.Context(),
		`TRUNCATE `+tableName,
	)
	if err != nil {
		tb.Errorf("truncate %s: %v", tableName, err)
	}
}

func newPGStore(tb testing.TB, url string, tableName string) task.Store {
	tb.Helper()
	pool, err := pgxpool.New(tb.Context(), url)
	if err != nil {
		tb.Fatalf("connect to db: %v", err)
	}
	tb.Cleanup(func() { pool.Close() })

	truncateTable(tb, pool, tableName)
	return NewTaskStore(pool)
}

// newAuthPool connects to the test database and removes every user except the
// legacy one that the migrations seed (tasks reference it), which also removes
// their sessions and tokens through ON DELETE CASCADE.
func newAuthPool(tb testing.TB, url string) *pgxpool.Pool {
	tb.Helper()
	pool, err := pgxpool.New(tb.Context(), url)
	if err != nil {
		tb.Fatalf("connect to db: %v", err)
	}
	tb.Cleanup(func() { pool.Close() })

	truncateTable(tb, pool, "sessions, user_tokens")
	_, err = pool.Exec(
		tb.Context(),
		`DELETE FROM users WHERE id <> '00000000-0000-0000-0000-000000000000'`,
	)
	if err != nil {
		tb.Fatalf("delete users: %v", err)
	}
	return pool
}

// newAuthQueries is newAuthPool for the stores that take a *sqlcgen.Queries.
func newAuthQueries(tb testing.TB, url string) *sqlcgen.Queries {
	tb.Helper()
	return sqlcgen.New(newAuthPool(tb, url))
}
