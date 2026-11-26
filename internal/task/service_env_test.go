package task_test

import (
	"context"
	"crypto/sha256"
	"os"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/opaquetoken"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.RunMain(m)) }

var (
	poolOnce sync.Once
	poolVal  *pgxpool.Pool
	errPool  error
)

// testPool is the pool on this package's private database, shared by all tests
// (it is released when the database is dropped at the end of the run).
func testPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	url := testutil.DatabaseURL(tb)
	poolOnce.Do(func() { poolVal, errPool = pgxpool.New(context.Background(), url) })
	if errPool != nil {
		tb.Fatalf("connect to the test database: %v", errPool)
	}
	return poolVal
}

// --- fakes: only for things that are not Postgres ----------------------------

// fakeOwnerLimiter is a [task.OwnerInviteLimiter] that records its calls.
type fakeOwnerLimiter struct {
	mu         sync.Mutex
	calls      int
	retryAfter time.Duration
	err        error
}

func (f *fakeOwnerLimiter) Allow(context.Context, uuid.UUID) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.retryAfter, f.err
}

func (f *fakeOwnerLimiter) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeMailLimiter is a [task.InviteMailLimiter] that records its calls.
type fakeMailLimiter struct {
	mu         sync.Mutex
	calls      []string
	retryAfter time.Duration
	err        error
}

func (f *fakeMailLimiter) Allow(_ context.Context, mail string) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, mail)
	return f.retryAfter, f.err
}

func (f *fakeMailLimiter) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// --- the environment ---------------------------------------------------------

// svcEnv is a [task.Service] on real Postgres plus direct access to the same
// stores for seeding and for checking what the service did. Tests only touch
// rows they created, so they never depend on empty tables.
type svcEnv struct {
	pool        *pgxpool.Pool
	tasks       *postgres.TaskStore
	members     *postgres.TaskMemberStore
	invitations *postgres.TaskInvitationStore
	users       *postgres.UserStore

	owner *fakeOwnerLimiter
	mail  *fakeMailLimiter
	reg   *prometheus.Registry
	met   *metrics.Task
	svc   *task.Service
}

func newSvcEnv(t *testing.T) *svcEnv {
	t.Helper()
	pool := testPool(t)
	q := sqlcgen.New(pool)
	e := &svcEnv{
		pool:        pool,
		tasks:       postgres.NewTaskStore(q),
		members:     postgres.NewMemberStore(q),
		invitations: postgres.NewInvitationStore(q),
		users:       postgres.NewUserStore(q),
		owner:       &fakeOwnerLimiter{},
		mail:        &fakeMailLimiter{},
		reg:         prometheus.NewRegistry(),
	}
	e.met = metrics.NewTask(e.reg)
	e.svc = e.build(e.owner, e.mail)
	return e
}

// build makes a service on the real stores with the given limiters.
func (e *svcEnv) build(owner task.OwnerInviteLimiter, mail task.InviteMailLimiter) *task.Service {
	return task.NewService(task.ServiceDeps{
		Tx:                 postgres.NewTransactor(e.pool, postgres.NewTaskStores),
		Tasks:              e.tasks,
		Invitations:        e.invitations,
		Members:            e.members,
		Users:              e.users,
		Outbox:             postgres.NewOutboxStore(sqlcgen.New(e.pool)),
		InviteMailLimiter:  mail,
		OwnerInviteLimiter: owner,
		Metrics:            e.met,
	})
}

// user persists a verified user.
func (e *svcEnv) user(t *testing.T) auth.User {
	t.Helper()
	u := authtest.SeedUser(t.Context(), t, e.users)
	if err := e.users.SetEmailVerified(t.Context(), u.ID, time.Now()); err != nil {
		t.Fatalf("verify user: %v", err)
	}
	return u
}

// unverifiedUser persists a user whose email is not verified.
func (e *svcEnv) unverifiedUser(t *testing.T) auth.User {
	t.Helper()
	return authtest.SeedUser(t.Context(), t, e.users)
}

// disabledUser persists a verified user that is disabled.
func (e *svcEnv) disabledUser(t *testing.T) auth.User {
	t.Helper()
	u := e.user(t)
	if _, err := e.pool.Exec(t.Context(), `UPDATE users SET disabled_at = now() WHERE id = $1`, u.ID); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	return u
}

// task persists a task owned by owner.
func (e *svcEnv) task(t *testing.T, owner uuid.UUID) task.Task {
	t.Helper()
	nt, err := task.New(task.NewParams{Title: "shared", OwnerID: owner, Status: task.StatusTodo}, time.Now())
	if err != nil {
		t.Fatalf("task.New: %v", err)
	}
	created, err := e.tasks.Create(t.Context(), nt)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	return created
}

func hashOfNew() []byte {
	sum := sha256.Sum256([]byte(uuid.NewV7().String()))
	return sum[:]
}

// pending persists a pending invitation created at createdAt.
func (e *svcEnv) pending(t *testing.T, taskID uuid.UUID, mail string, role task.Role, createdAt time.Time) task.Invitation {
	t.Helper()
	inv, err := task.NewInvitation(task.NewInvitationParams{TaskID: taskID, Email: mail, Role: role}, createdAt)
	if err != nil {
		t.Fatalf("task.NewInvitation: %v", err)
	}
	inv.TokenHash = hashOfNew()
	if err := e.invitations.Create(t.Context(), inv); err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	return inv
}

// closed persists an invitation with the given non-pending status.
func (e *svcEnv) closed(t *testing.T, taskID uuid.UUID, mail string, status task.InvitationStatus) task.Invitation {
	t.Helper()
	inv := e.pending(t, taskID, mail, task.RoleViewer, time.Now())
	if err := e.invitations.SetStatus(t.Context(), inv.ID, status, time.Now()); err != nil {
		t.Fatalf("set status: %v", err)
	}
	inv.Status = status
	return inv
}

// token gives inv a fresh token, the way the mail builder does, and returns
// the string that goes into the link.
func (e *svcEnv) token(t *testing.T, inv task.Invitation) string {
	t.Helper()
	raw, err := opaquetoken.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	if err := e.invitations.SetTokenHash(t.Context(), inv.ID, opaquetoken.Hash(raw), time.Now()); err != nil {
		t.Fatalf("set token hash: %v", err)
	}
	return opaquetoken.Encode(raw)
}

// member makes a new verified user a member of the task with role.
func (e *svcEnv) member(t *testing.T, taskID uuid.UUID, role task.Role) auth.User {
	t.Helper()
	u := e.user(t)
	e.addMember(t, taskID, u, role)
	return u
}

// addMember makes u a member of the task: a member comes from an accepted invitation.
func (e *svcEnv) addMember(t *testing.T, taskID uuid.UUID, u auth.User, role task.Role) {
	t.Helper()
	inv := e.closed(t, taskID, u.Email, task.InvitationStatusAccepted)
	m, err := task.NewMember(task.NewMemberParams{TaskID: taskID, UserID: u.ID, Role: role, InvitationID: inv.ID}, time.Now())
	if err != nil {
		t.Fatalf("task.NewMember: %v", err)
	}
	if err := e.members.Create(t.Context(), m); err != nil {
		t.Fatalf("create member: %v", err)
	}
}

// fillMembers adds members until the task has n of them.
func (e *svcEnv) fillMembers(t *testing.T, taskID uuid.UUID, n int) {
	t.Helper()
	for range n {
		e.member(t, taskID, task.RoleViewer)
	}
}

type outboxRow struct {
	Kind  string
	RefID *uuid.UUID
}

// outbox returns the outbox rows queued for the address.
func (e *svcEnv) outbox(t *testing.T, to string) []outboxRow {
	t.Helper()
	rows, err := e.pool.Query(t.Context(), `SELECT kind, ref_id FROM email_outbox WHERE to_email = $1 ORDER BY created_at, id`, to)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.Kind, &r.RefID); err != nil {
			t.Fatalf("scan outbox: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("outbox rows: %v", err)
	}
	return out
}

// count returns the value of a counter with one label, 0 when it was never touched.
func (e *svcEnv) count(t *testing.T, name, label, value string) float64 {
	t.Helper()
	families, err := e.reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == label && l.GetValue() == value {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

func (e *svcEnv) invitationCount(t *testing.T, name string) float64 {
	t.Helper()
	return e.count(t, "task_invitations_total", "event", name)
}

func (e *svcEnv) denied(t *testing.T, permission string) float64 {
	t.Helper()
	return e.count(t, "task_authz_denied_total", "permission", permission)
}

// memberCount returns how many members the task has.
func (e *svcEnv) memberCount(t *testing.T, taskID uuid.UUID) int64 {
	t.Helper()
	n, err := e.members.Count(t.Context(), taskID)
	if err != nil {
		t.Fatalf("count members: %v", err)
	}
	return n
}

// assignee returns the assignee of the task, nil when there is none.
func (e *svcEnv) assignee(t *testing.T, ownerID, taskID uuid.UUID) *uuid.UUID {
	t.Helper()
	a, err := e.tasks.Get(t.Context(), ownerID, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	return a.Task.AssigneeID
}

// invitationByToken fetches the invitation that token points at.
func (e *svcEnv) invitationByToken(t *testing.T, token string) task.Invitation {
	t.Helper()
	raw, err := opaquetoken.Decode(token)
	if err != nil {
		t.Fatalf("decode token: %v", err)
	}
	inv, err := e.invitations.GetByTokenHash(t.Context(), opaquetoken.Hash(raw))
	if err != nil {
		t.Fatalf("get invitation: %v", err)
	}
	return inv
}
