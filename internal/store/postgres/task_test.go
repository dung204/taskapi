package postgres

import (
	"context"
	"os"
	"sync"

	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

var migrateOnce sync.Once

func migrateTestDB(tb testing.TB, pool *pgxpool.Pool) {
	tb.Helper()
	migrateOnce.Do(func() {
		db := stdlib.OpenDBFromPool(pool)
		provider, err := goose.NewProvider(
			goose.DialectPostgres, db, os.DirFS("../../../migrations"),
		)
		if err != nil {
			tb.Fatalf("goose provider: %v", err)
		}

		if _, err := provider.Up(context.Background()); err != nil {
			tb.Fatalf("migrate: %v", err)
		}
	})
}

func truncateTasks(tb testing.TB, pool *pgxpool.Pool) {
	tb.Helper()

	_, err := pool.Exec(
		tb.Context(),
		`TRUNCATE tasks`,
	)
	if err != nil {
		tb.Errorf("truncate tasks: %v", err)
	}
}

func newPGStore(tb testing.TB, url string) task.Store {
	tb.Helper()
	pool, err := pgxpool.New(tb.Context(), url)
	if err != nil {
		tb.Fatalf("connect to db: %v", err)
	}
	tb.Cleanup(func() { pool.Close() })

	migrateTestDB(tb, pool)
	truncateTasks(tb, pool)
	return NewTaskStore(pool)
}

func TestTaskStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.TestStore(t, func(t *testing.T) task.Store {
		t.Helper()
		return newPGStore(t, url)
	})
}

func BenchmarkTaskStore(b *testing.B) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		b.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.BenchmarkStore(b, func(tb testing.TB) task.Store {
		tb.Helper()
		return newPGStore(tb, url)
	})
}
