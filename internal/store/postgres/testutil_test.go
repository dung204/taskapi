package postgres

import (
	"os"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
)

// TestMain clones the migrated template database for this package (see
// testutil.RunMain); the tests run on the clone, which is dropped afterwards.
func TestMain(m *testing.M) { os.Exit(testutil.RunMain(m)) }

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

	// Invitations and members reference tasks, and TRUNCATE refuses to empty a
	// referenced table unless the referencing ones are truncated with it.
	if tableName == "tasks" {
		tableName = "task_members, task_invitations, tasks"
	}
	truncateTable(tb, pool, tableName)
	return NewTaskStore(sqlcgen.New(pool))
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

// newShareDeps gives the invitation and member suites empty stores. It clears
// tasks (and with them invitations and members) and removes the users of the
// previous test.
func newShareDeps(t *testing.T, url string) ShareDeps {
	t.Helper()
	pool := newAuthPool(t, url)
	truncateTable(t, pool, "task_members, task_invitations, tasks")

	q := sqlcgen.New(pool)
	users := NewUserStore(q)
	tasks := NewTaskStore(q)

	newUser := func() (uuid.UUID, string) {
		u := authtest.SeedUser(t.Context(), t, users)
		return u.ID, u.Email
	}
	newTask := func() (task.Task, string) {
		ownerID, ownerEmail := newUser()
		nt, err := task.New(task.NewParams{Title: "shared task", OwnerID: ownerID, Status: task.StatusTodo}, time.Now())
		if err != nil {
			t.Fatalf("task.New: %v", err)
		}
		created, err := tasks.Create(t.Context(), nt)
		if err != nil {
			t.Fatalf("create task: %v", err)
		}
		return created, ownerEmail
	}

	return ShareDeps{
		Invitations: NewInvitationStore(q),
		Members:     NewMemberStore(q),
		NewTask:     newTask,
		NewUser:     newUser,
	}
}
