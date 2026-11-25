package httpapi

import (
	"crypto/sha256"
	"os"
	"sync"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/opaquetoken"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
	"github.com/dung204/taskapi/internal/testutil/logtest"
)

func TestMain(m *testing.M) { os.Exit(testutil.RunMain(m)) }

var (
	poolOnce   sync.Once
	sharedPool *pgxpool.Pool
	errPool    error
)

// testPool returns the pool on this package's private database, shared by all
// tests (a pool per test would exhaust the server's connections once tests run
// in parallel). It is never closed: dropping the database at the end of the run
// disconnects it.
func testPool(tb testing.TB) *pgxpool.Pool {
	tb.Helper()
	url := testutil.DatabaseURL(tb)
	poolOnce.Do(func() { sharedPool, errPool = pgxpool.New(context.Background(), url) })
	if errPool != nil {
		tb.Fatalf("connect to the test database: %v", errPool)
	}
	return sharedPool
}

// env is the real Postgres stores of one test, plus the user the handler
// authenticates as. Tests share the database but only touch rows they created
// (unique ids and emails), so they never depend on empty tables.
type env struct {
	tasks       *postgres.TaskStore
	members     *postgres.TaskMemberStore
	invitations *postgres.TaskInvitationStore
	users       *postgres.UserStore
	outbox      *postgres.OutboxStore
	tx          *postgres.Transactor[task.Stores]

	// caller is the user the handlers of this env authenticate as.
	caller      uuid.UUID
	callerEmail string

	// The invitation limiters are fakes (Redis is not Postgres); a test sets
	// ownerLimit.retryAfter to make the hourly owner cap refuse.
	ownerLimit *fakeOwnerLimiter
	mailLimit  allowAllMail
	reg        *prometheus.Registry
	met        *metrics.Task
}

type fakeOwnerLimiter struct {
	mu         sync.Mutex
	calls      int
	retryAfter time.Duration
}

func (f *fakeOwnerLimiter) Allow(context.Context, uuid.UUID) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.retryAfter, nil
}

type allowAllMail struct{}

func (allowAllMail) Allow(context.Context, string) (time.Duration, error) { return 0, nil }

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testPool(t)
	q := sqlcgen.New(pool)
	e := &env{
		tasks:       postgres.NewTaskStore(q),
		members:     postgres.NewMemberStore(q),
		invitations: postgres.NewInvitationStore(q),
		users:       postgres.NewUserStore(q),
		outbox:      postgres.NewOutboxStore(q),
		tx:          postgres.NewTransactor(pool, postgres.NewTaskStores),
		ownerLimit:  &fakeOwnerLimiter{},
		mailLimit:   allowAllMail{},
		reg:         prometheus.NewRegistry(),
	}
	e.met = metrics.NewTask(e.reg)
	u := e.newUser(t)
	if err := e.users.SetEmailVerified(t.Context(), u.ID, time.Now()); err != nil {
		t.Fatalf("verify the caller: %v", err)
	}
	e.caller, e.callerEmail = u.ID, u.Email
	return e
}

// newUser persists a user with a unique email.
func (e *env) newUser(t *testing.T) auth.User {
	t.Helper()
	return authtest.SeedUser(t.Context(), t, e.users)
}

// seedTask persists a task owned by owner; mod (optional) adjusts it before it
// is stored. The ID is unique.
func (e *env) seedTask(t *testing.T, owner uuid.UUID, mod func(*task.NewParams)) task.Task {
	t.Helper()
	p := task.NewParams{
		Title:       "title",
		Description: new("description"),
		OwnerID:     owner,
		Status:      task.StatusDoing,
		DueAt:       new(time.Date(2026, 10, 14, 9, 53, 50, 0, time.UTC)),
	}
	if mod != nil {
		mod(&p)
	}
	nt, err := task.New(p, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("task.New: %v", err)
	}
	created, err := e.tasks.Create(t.Context(), nt)
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return created
}

// addMember makes userID a member of the task with the given role. A member
// comes from an accepted invitation, so it stores one first.
func (e *env) addMember(t *testing.T, taskID, userID uuid.UUID, mail string, role task.Role) {
	t.Helper()
	now := time.Now()
	inv, err := task.NewInvitation(task.NewInvitationParams{TaskID: taskID, Email: mail, Role: role}, now)
	if err != nil {
		t.Fatalf("task.NewInvitation: %v", err)
	}
	sum := sha256.Sum256([]byte(uuid.NewV7().String()))
	inv.Status = task.InvitationStatusAccepted
	inv.TokenHash = sum[:]
	if err := e.invitations.Create(t.Context(), inv); err != nil {
		t.Fatalf("seed invitation: %v", err)
	}
	m, err := task.NewMember(task.NewMemberParams{TaskID: taskID, UserID: userID, Role: role, InvitationID: inv.ID}, now)
	if err != nil {
		t.Fatalf("task.NewMember: %v", err)
	}
	if err := e.members.Create(t.Context(), m); err != nil {
		t.Fatalf("seed member: %v", err)
	}
}

// service returns a [task.Service] on the real stores and transactions. tasks
// replaces the task store the service reads and writes directly (nil keeps the
// real one): tests pass a thin decorator that injects errors or records calls.
func (e *env) service(tasks task.Store) *task.Service {
	if tasks == nil {
		tasks = e.tasks
	}
	return task.NewService(task.ServiceDeps{
		Tx:          e.tx,
		Tasks:       tasks,
		Invitations: e.invitations,
		Members:     e.members,
		Users:       e.users,
		Outbox:      e.outbox,

		InviteMailLimiter:  e.mailLimit,
		OwnerInviteLimiter: e.ownerLimit,
		Metrics:            e.met,
	})
}

// handler builds the handler for the task tests, authenticated as e.caller.
func (e *env) handler(t *testing.T, tasks task.Store, logger *slog.Logger) http.Handler {
	t.Helper()
	return withTestAuthAs(t, e.caller, HandlerDeps{Tasks: e.service(tasks), Logger: logger})
}

// setupTest returns a handler on real stores, the env behind it and the log recorder.
func setupTest(t *testing.T) (h http.Handler, e *env, rec *logtest.Recorder) {
	t.Helper()
	return setupTestWithStore(t, nil)
}

// setupTestWithStore is like setupTest with a decorator around the real task
// store (nil for none). The decorator embeds the store the test passes it, so
// build it from newEnv instead when it must delegate: see [setupTestWith].
func setupTestWithStore(t *testing.T, tasks task.Store) (http.Handler, *env, *logtest.Recorder) {
	t.Helper()
	return setupTestWith(t, newEnv(t), tasks)
}

func setupTestWith(t *testing.T, e *env, tasks task.Store) (http.Handler, *env, *logtest.Recorder) {
	t.Helper()
	rec := logtest.NewRecorder()
	logger := slog.New(NewRequestIDContextHandler(rec))
	return e.handler(t, tasks, logger), e, rec
}

// newTestService returns a [task.Service] on real stores, for tests that do
// not care who the caller is.
func newTestService(t *testing.T, tasks task.Store) *task.Service {
	t.Helper()
	return newEnv(t).service(tasks)
}

// withTestAuth is [withTestAuthAs] for a caller that has no row in the database:
// enough for requests that never write on its behalf.
func withTestAuth(t *testing.T, deps HandlerDeps) http.Handler {
	t.Helper()
	return withTestAuthAs(t, uuid.NewV7(), deps)
}

// withTestAuthAs builds a handler from deps with a real token verifier and an
// empty revocation list. The task endpoints need a bearer token, so requests
// that carry no Authorization header get a valid one for userID; the auth rules
// themselves are tested through setupAuthTest.
func withTestAuthAs(t *testing.T, userID uuid.UUID, deps HandlerDeps) http.Handler {
	t.Helper()

	pub, priv := newTestKeys(t)
	jwtService := auth.NewJWTService(auth.JWTServiceOpts{
		PrivateKey: priv, PublicKey: pub,
		Issuer: "taskapi-test", Audiences: []string{"taskapi-test"}, TTL: time.Hour,
	})

	token, _, err := jwtService.Issue(userID, uuid.NewV7(), time.Now())
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}

	deps.Verifier = jwtService
	deps.Revoker = newFakeRevoker()
	h := NewHandler(deps)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		h.ServeHTTP(w, r)
	})
}

func prepareTestRequest(tb testing.TB, method, target string, body any) (*httptest.ResponseRecorder, *http.Request) {
	tb.Helper()

	var r io.Reader
	switch v := body.(type) {
	case nil:

	case string:
		r = strings.NewReader(v)

	default:
		b, err := json.Marshal(v)
		if err != nil {
			tb.Fatalf("marshal body: %v", err)
		}

		r = bytes.NewReader(b)
	}

	req := httptest.NewRequestWithContext(tb.Context(), method, target, r)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	return rec, req
}

func decodeResponseBody[T any](tb testing.TB, body io.ReadCloser) T {
	tb.Helper()

	var dst T
	err := json.NewDecoder(body).Decode(&dst)
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError

	if err != nil {
		switch {
		case errors.Is(err, io.EOF):
			tb.Fatalf("response body is required")
		case errors.Is(err, io.ErrUnexpectedEOF):
			tb.Fatalf("response body contains malformed JSON")
		case errors.As(err, &syntaxErr):
			tb.Fatalf("request body contains malformed JSON (at position %d)", syntaxErr.Offset)
		case errors.As(err, &typeErr):
			if typeErr.Field == "" {
				tb.Fatalf("response body must be a JSON object")
			}
			tb.Fatalf("field %q has the wrong type", typeErr.Field)
		default:
			tb.Fatal("invalid response body")
		}
	}

	return dst
}

func getErrorMessageFromResponseBody(tb testing.TB, body io.ReadCloser) string {
	tb.Helper()
	return decodeResponseBody[oapigen.Error](tb, body).Error
}

func checkErrorMesssage(tb testing.TB, msg string, wantErrContains []string) {
	tb.Helper()
	if len(wantErrContains) == 0 {
		tb.Fatalf("case %q: error case must set wantErrContains", tb.Name())
	}

	gotErrMes := strings.ToLower(msg)
	for _, want := range wantErrContains {
		if !strings.Contains(gotErrMes, strings.ToLower(want)) {
			tb.Errorf("error message: %q, want error message contains %q", gotErrMes, want)
		}
	}
}

func checkErrorMessageHidden(tb testing.TB, msg string, secrets []string) {
	tb.Helper()
	if len(secrets) == 0 {
		tb.Fatalf("case %q: error case must set wantErrContains", tb.Name())
	}

	lower := strings.ToLower(msg)
	for _, s := range secrets {
		if s == "" {
			tb.Fatalf("checkErrorMessageHidden: empty secret would match any body")
		}
		if strings.Contains(lower, strings.ToLower(s)) {
			tb.Errorf("response body leaks %q: %s", s, msg)
		}
	}
}

// verifiedUser persists a user whose email is verified.
func (e *env) verifiedUser(t *testing.T) auth.User {
	t.Helper()
	u := e.newUser(t)
	if err := e.users.SetEmailVerified(t.Context(), u.ID, time.Now()); err != nil {
		t.Fatalf("verify user: %v", err)
	}
	return u
}

// asMember makes e.caller a member of a new task owned by somebody else, with role.
func (e *env) asMember(t *testing.T, role task.Role) (tk task.Task, owner auth.User) {
	t.Helper()
	owner = e.verifiedUser(t)
	tk = e.seedTask(t, owner.ID, nil)
	e.addMember(t, tk.ID, e.caller, e.callerEmail, role)
	return tk, owner
}

// pendingFor stores a pending invitation for mail and returns it with the
// token string that answers it (the mail builder normally makes the token).
func (e *env) pendingFor(t *testing.T, taskID uuid.UUID, mail string, role task.Role) (task.Invitation, string) {
	t.Helper()
	inv, err := task.NewInvitation(task.NewInvitationParams{TaskID: taskID, Email: mail, Role: role}, time.Now())
	if err != nil {
		t.Fatalf("task.NewInvitation: %v", err)
	}
	raw, err := opaquetoken.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	inv.TokenHash = opaquetoken.Hash(raw)
	if err := e.invitations.Create(t.Context(), inv); err != nil {
		t.Fatalf("seed invitation: %v", err)
	}
	return inv, opaquetoken.Encode(raw)
}

// denied returns task_authz_denied_total{permission}.
func (e *env) denied(t *testing.T, permission string) float64 {
	t.Helper()
	families, err := e.reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != "task_authz_denied_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "permission" && l.GetValue() == permission {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}
