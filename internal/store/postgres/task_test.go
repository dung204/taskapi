package postgres

import (
	"os"
	"strings"
	"testing"
	"uuid"

	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

func TestTaskStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.TestStore(t, func(t *testing.T) (task.Store, func() uuid.UUID) {
		t.Helper()
		pool := newAuthPool(t, url)
		truncateTable(t, pool, "tasks")

		users := NewUserStore(sqlcgen.New(pool))
		newOwner := func() uuid.UUID { return authtest.SeedUser(t.Context(), t, users).ID }

		return NewTaskStore(pool), newOwner
	})
}

func BenchmarkTaskStore(b *testing.B) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		b.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.BenchmarkStore(b, func(tb testing.TB) task.Store {
		tb.Helper()
		return newPGStore(tb, url, "tasks")
	})
}
