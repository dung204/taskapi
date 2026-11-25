package postgres

import (
	"testing"
	"uuid"

	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
)

func TestTaskStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	runTaskStoreSuite(t, func(t *testing.T) (task.Store, func() uuid.UUID) {
		t.Helper()
		pool := newAuthPool(t, url)
		truncateTable(t, pool, "task_members, task_invitations, tasks")

		users := NewUserStore(sqlcgen.New(pool))
		newOwner := func() uuid.UUID { return authtest.SeedUser(t.Context(), t, users).ID }

		return NewTaskStore(sqlcgen.New(pool)), newOwner
	})
}

func BenchmarkTaskStore(b *testing.B) {
	url := testutil.DatabaseURL(b)
	runStoreBenchmark(b, func(tb testing.TB) task.Store {
		tb.Helper()
		return newPGStore(tb, url, "tasks")
	})
}
