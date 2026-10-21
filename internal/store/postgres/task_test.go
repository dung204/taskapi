package postgres

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func truncateTasks(t *testing.T, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(
		t.Context(),
		`TRUNCATE tasks`,
	)
	if err != nil {
		t.Errorf("truncate tasks: %v", err)
	}
}

func newPGStore(t *testing.T, url string) task.Store {
	t.Helper()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("connect to db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	truncateTasks(t, db)
	return NewTaskStore(db)
}

func TestTaskStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.TestStore(t, func(t *testing.T) task.Store {
		return newPGStore(t, url)
	})
}
