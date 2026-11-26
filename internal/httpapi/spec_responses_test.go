package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/httpapi/docs"
	"github.com/dung204/taskapi/internal/task"
)

// This file checks every (operation, status code) pair that openapi.yml
// declares for the task and sharing endpoints: each has at least one case, and
// the error message of each case equals an `example` of the spec. Conditions
// that the service layer owns (caps, races, branches) are tested there; a case
// here only shows that the outcome maps to the right status, message and headers.

// specOp is one operation of the spec.
type specOp struct {
	ID, Method, Path string
	// Messages maps a status code to example name -> error message.
	Messages map[int]map[string]string
	Statuses []int
}

var (
	specOnce sync.Once
	specOps  map[string]specOp
	errSpec  error
)

// loadSpec reads the operations under /tasks and /invitations.
func loadSpec(t *testing.T) map[string]specOp {
	t.Helper()
	specOnce.Do(func() {
		rec := httptest.NewRecorder()
		docs.Spec(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/openapi.yaml", nil))
		doc, err := openapi3.NewLoader().LoadFromData(rec.Body.Bytes())
		if err != nil {
			errSpec = err
			return
		}
		specOps = map[string]specOp{}
		for path, item := range doc.Paths.Map() {
			if !strings.HasPrefix(path, "/tasks") && !strings.HasPrefix(path, "/invitations") {
				continue
			}
			for method, op := range item.Operations() {
				so := specOp{ID: op.OperationID, Method: method, Path: path, Messages: map[int]map[string]string{}}
				for code, ref := range op.Responses.Map() {
					var status int
					if _, err := fmt.Sscanf(code, "%d", &status); err != nil {
						errSpec = fmt.Errorf("status %q of %s: %w", code, op.OperationID, err)
						return
					}
					so.Statuses = append(so.Statuses, status)
					media := ref.Value.Content["application/json"]
					if media == nil || status < 400 {
						continue
					}
					so.Messages[status] = map[string]string{}
					if m, ok := media.Example.(map[string]any); ok {
						if msg, ok := m["error"].(string); ok {
							so.Messages[status]["example"] = msg
						}
					}
					for name, ex := range media.Examples {
						if m, ok := ex.Value.Value.(map[string]any); ok {
							if msg, ok := m["error"].(string); ok {
								so.Messages[status][name] = msg
							}
						}
					}
				}
				slices.Sort(so.Statuses)
				specOps[so.ID] = so
			}
		}
	})
	if errSpec != nil {
		t.Fatalf("load openapi.yml: %v", errSpec)
	}
	return specOps
}

// call is the request of a case.
type call struct {
	caller       uuid.UUID // zero means e.caller
	method, path string
	body         any
	header       map[string]string
	tweak        func(d *HandlerDeps) // changes the handler's dependencies
	wantHeaders  map[string]string
}

type httpCase struct {
	op      string
	status  int
	example string // the spec example whose message the body must equal; "" accepts any of the status
	name    string
	setup   func(t *testing.T, e *env) call
	check   func(t *testing.T, e *env, body []byte) // optional extra checks on a successful body
}

func (c httpCase) title() string {
	return fmt.Sprintf("%s/%d/%s%s", c.op, c.status, c.example, c.name)
}

// failingTx fails every transaction, to reach the 500 and 503 of operations
// that start with one.
type failingTx struct{ err error }

func (f failingTx) WithTx(context.Context, func(task.Stores) error) error { return f.err }

// failingInvitations fails the invitation lookup that accepting and declining
// do before they open a transaction.
type failingInvitations struct {
	task.InvitationStore
	err error
}

func (f failingInvitations) GetByTokenHash(context.Context, []byte) (task.Invitation, error) {
	return task.Invitation{}, f.err
}

func runCase(t *testing.T, ops map[string]specOp, c httpCase) {
	t.Helper()
	op, ok := ops[c.op]
	if !ok {
		t.Fatalf("case refers to unknown operation %q", c.op)
	}

	e := newEnv(t)
	cl := c.setup(t, e)
	caller := cl.caller
	if caller == (uuid.UUID{}) {
		caller = e.caller
	}
	deps := HandlerDeps{Tasks: e.service(nil), Logger: slog.New(slog.DiscardHandler)}
	if cl.tweak != nil {
		cl.tweak(&deps)
	}
	h := withTestAuthAs(t, caller, deps)

	rec, req := prepareTestRequest(t, cl.method, cl.path, cl.body)
	for k, v := range cl.header {
		req.Header.Set(k, v)
	}
	h.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)

	if res.StatusCode != c.status {
		t.Fatalf("%s %s: status = %d, want %d (body %s)", cl.method, cl.path, res.StatusCode, c.status, body)
	}
	if c.status >= 400 {
		var got struct{ Error string }
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("error body %q: %v", body, err)
		}
		want := op.Messages[c.status]
		if c.example != "" {
			msg, ok := want[c.example]
			if !ok {
				t.Fatalf("spec has no example %q for %s %d", c.example, c.op, c.status)
			}
			if got.Error != msg {
				t.Errorf("message = %q, want the spec example %q: %q", got.Error, c.example, msg)
			}
		} else if !slices.Contains(mapValues(want), got.Error) {
			t.Errorf("message = %q, want one of the spec examples %v", got.Error, mapValues(want))
		}
	}
	for k, v := range cl.wantHeaders {
		if got := res.Header.Get(k); got != v {
			t.Errorf("header %s = %q, want %q", k, got, v)
		}
	}
	if c.check != nil {
		c.check(t, e, body)
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

func TestSpecResponses(t *testing.T) {
	t.Parallel()
	ops := loadSpec(t)
	for _, c := range append(explicitCases(), genericCases(ops)...) {
		t.Run(c.title(), func(t *testing.T) { runCase(t, ops, c) })
	}
}

// TestEveryResponseOfTheSpecHasACase fails when openapi.yml declares a status
// for an operation that no case exercises. It needs no database.
func TestEveryResponseOfTheSpecHasACase(t *testing.T) {
	t.Parallel()
	ops := loadSpec(t)
	covered := map[string]bool{}
	for _, c := range append(explicitCases(), genericCases(ops)...) {
		covered[fmt.Sprintf("%s/%d", c.op, c.status)] = true
	}
	for _, op := range ops {
		for _, st := range op.Statuses {
			if !covered[fmt.Sprintf("%s/%d", op.ID, st)] {
				t.Errorf("%s %s (%s): no test for status %d", op.Method, op.Path, op.ID, st)
			}
		}
	}
	for key := range covered {
		op, st, _ := strings.Cut(key, "/")
		var code int
		_, _ = fmt.Sscanf(st, "%d", &code)
		if o, ok := ops[op]; !ok || !slices.Contains(o.Statuses, code) {
			t.Errorf("a case tests %s with status %d, which the spec does not declare", op, code)
		}
	}
}

// --- generic cases: the same for every operation -------------------------------

var opBodies = map[string]any{
	"createTask":        map[string]any{"title": "x"},
	"updateTask":        map[string]any{"title": "y"},
	"setAssignee":       map[string]any{"email": "a@example.com"},
	"inviteToTask":      map[string]any{"email": "a@example.com", "role": "viewer"},
	"changeMemberRole":  map[string]any{"role": "viewer"},
	"acceptInvitation":  map[string]any{"token": "A2E7Iw6mKZ0ZB3V9QJxqk0cL8wH4o1nYd5rTgS_UfXc"},
	"declineInvitation": map[string]any{"token": "A2E7Iw6mKZ0ZB3V9QJxqk0cL8wH4o1nYd5rTgS_UfXc"},
}

func anyRequest(op specOp) call {
	path := op.Path
	for _, p := range []string{"{id}", "{invitationId}", "{userId}"} {
		path = strings.ReplaceAll(path, p, uuid.NewV7().String())
	}
	return call{method: op.Method, path: path, body: opBodies[op.ID]}
}

// genericCases covers the statuses that every operation shares: 401, 429, 500, 503.
func genericCases(ops map[string]specOp) []httpCase {
	var out []httpCase
	for _, op := range ops {
		out = append(out,
			httpCase{op: op.ID, status: http.StatusUnauthorized, example: "unauthorized", setup: func(t *testing.T, e *env) call {
				t.Helper()
				c := anyRequest(op)
				c.header = map[string]string{"Authorization": "Bearer garbage"}
				c.wantHeaders = map[string]string{"WWW-Authenticate": "Bearer"}
				return c
			}},
			httpCase{op: op.ID, status: http.StatusTooManyRequests, example: "tooManyRequests", setup: func(t *testing.T, e *env) call {
				t.Helper()
				c := anyRequest(op)
				c.tweak = func(d *HandlerDeps) { d.Limiter = &fakeLimiter{ok: false, retryAfter: 1500 * time.Millisecond} }
				c.wantHeaders = map[string]string{"Retry-After": "2"}
				return c
			}},
			httpCase{op: op.ID, status: http.StatusInternalServerError, setup: failing(op, errors.New("oops"))},
			httpCase{op: op.ID, status: http.StatusServiceUnavailable, setup: failing(op, apperr.ErrServiceUnavailable)},
		)
	}
	return out
}

func failing(op specOp, err error) func(*testing.T, *env) call {
	return func(t *testing.T, e *env) call {
		t.Helper()
		c := anyRequest(op)
		c.tweak = func(d *HandlerDeps) {
			d.Tasks = task.NewService(task.ServiceDeps{
				Tx: failingTx{err}, Tasks: mockFailingStore{err: err}, Invitations: failingInvitations{err: err},
			})
		}
		return c
	}
}
