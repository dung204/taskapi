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

func migrateTestDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	migrateOnce.Do(func() {
		db := stdlib.OpenDBFromPool(pool)
		provider, err := goose.NewProvider(
			goose.DialectPostgres, db, os.DirFS("../../../migrations"),
		)
		if err != nil {
			t.Fatalf("goose provider: %v", err)
		}

		if _, err := provider.Up(context.Background()); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	})
}

func truncateTasks(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	_, err := pool.Exec(
		t.Context(),
		`TRUNCATE tasks`,
	)
	if err != nil {
		t.Errorf("truncate tasks: %v", err)
	}
}

func newPGStore(t *testing.T, url string) task.Store {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	migrateTestDB(t, pool)
	truncateTasks(t, pool)
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
