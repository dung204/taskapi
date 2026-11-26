package httpapi

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

// send runs one request as e.caller and returns the status and body.
func send(t *testing.T, e *env, method, path string, body any) (int, string) {
	t.Helper()
	h := withTestAuthAs(t, e.caller, HandlerDeps{Tasks: e.service(nil), Logger: discardLogger()})
	rec, req := prepareTestRequest(t, method, path, body)
	h.ServeHTTP(rec, req)
	res := rec.Result()
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

// TestOrderOfChecks pins the order in which the layers reject a request: the
// IP rate limit (429) first, then the token (401), then path/body decoding (400), not visible (404), missing permission (403), value
// errors (400/422/409), and last the hourly owner cap (429). A 404 means the
// caller cannot see the task; a 403 means they can see it but lack the flag.
func TestOrderOfChecks(t *testing.T) {
	t.Parallel()
	invite := func(email, role string) map[string]any { return map[string]any{"email": email, "role": role} }

	t.Run("the IP rate limit (429) comes before the token check (401)", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		for _, token := range []string{"Bearer garbage", ""} {
			deps := HandlerDeps{Tasks: e.service(nil), Logger: discardLogger(), Limiter: &fakeLimiter{ok: false, retryAfter: time.Second}}
			h := withTestAuthAs(t, e.caller, deps)
			rec, req := prepareTestRequest(t, "GET", taskPath(uuid.NewV7(), "/shares"), nil)
			if token != "" {
				req.Header.Set("Authorization", token)
			}
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("Authorization %q: status = %d, want 429 before 401", token, rec.Code)
			}
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Errorf("a 429 must not carry WWW-Authenticate")
			}
		}
	})

	t.Run("a malformed body is 400 even for a task the caller cannot see", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		st, _ := send(t, e, "POST", taskPath(stranger(t, e).ID, "/invitations"), `{"email":`)
		if st != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", st)
		}
	})

	t.Run("404 comes before 403 and before value errors", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		st, _ := send(t, e, "POST", taskPath(stranger(t, e).ID, "/invitations"), invite(e.callerEmail, "owner"))
		if st != http.StatusNotFound {
			t.Errorf("status = %d, want 404 (the caller cannot see the task)", st)
		}
	})

	t.Run("403 comes before value errors", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		tk, _ := e.asMember(t, task.RoleManager)
		st, _ := send(t, e, "POST", taskPath(tk.ID, "/invitations"), invite(e.callerEmail, "owner"))
		if st != http.StatusForbidden {
			t.Errorf("invite: status = %d, want 403", st)
		}

		tk, _ = e.asMember(t, task.RoleViewer)
		st, _ = send(t, e, "PATCH", taskPath(tk.ID, ""), map[string]any{"title": ""})
		if st != http.StatusForbidden {
			t.Errorf("patch: status = %d, want 403", st)
		}
	})

	t.Run("value errors come before the hourly owner cap", func(t *testing.T) {
		t.Parallel()
		e := newEnv(t)
		e.ownerLimit.retryAfter = time.Hour
		tk := ownedBy(t, e)
		for name, body := range map[string]map[string]any{
			"own email":    invite(e.callerEmail, "viewer"),
			"invalid role": invite("x@example.com", "owner"),
		} {
			if st, msg := send(t, e, "POST", taskPath(tk.ID, "/invitations"), body); st == http.StatusTooManyRequests {
				t.Errorf("%s: status = 429 (%s), want the value error first", name, msg)
			}
		}
		if e.ownerLimit.calls != 0 {
			t.Errorf("the owner limiter was consulted %d times by rejected requests, want 0", e.ownerLimit.calls)
		}
		if st, _ := send(t, e, "POST", taskPath(tk.ID, "/invitations"), invite("x@example.com", "viewer")); st != http.StatusTooManyRequests {
			t.Errorf("a valid request: status = %d, want 429", st)
		}
	})
}

// TestPermissionMatrix is generated from [task.Role.Can]: for every role,
// including the owner, an endpoint answers 403 exactly when the role lacks the
// permission it needs, and succeeds otherwise.
func TestPermissionMatrix(t *testing.T) {
	t.Parallel()
	roles := []task.Role{task.RoleOwner, task.RoleManager, task.RoleEditor, task.RoleContributor, task.RoleViewer}
	type row struct {
		name   string
		perm   task.Permission
		wantOK int
		// build prepares the targets on tk and returns the request.
		build func(t *testing.T, e *env, tk task.Task) (method, path string, body any)
	}
	rows := []row{
		{"get task", task.PermView, 200, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "GET", taskPath(tk.ID, ""), nil
		}},
		{"list shares", task.PermViewShares, 200, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "GET", taskPath(tk.ID, "/shares"), nil
		}},
		{"edit content", task.PermEditContent, 200, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "PATCH", taskPath(tk.ID, ""), map[string]any{"title": "new"}
		}},
		{"change status", task.PermChangeStatus, 200, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "PATCH", taskPath(tk.ID, ""), map[string]any{"status": "done"}
		}},
		{"set assignee", task.PermAssign, 200, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "PUT", taskPath(tk.ID, "/assignee"), map[string]any{"email": e.callerEmail}
		}},
		{"clear assignee", task.PermAssign, 204, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "DELETE", taskPath(tk.ID, "/assignee"), nil
		}},
		{"delete task", task.PermDelete, 204, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "DELETE", taskPath(tk.ID, ""), nil
		}},
		{"invite", task.PermManageShares, 201, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			return "POST", taskPath(tk.ID, "/invitations"), map[string]any{"email": "new-" + e.callerEmail, "role": "viewer"}
		}},
		{"cancel invitation", task.PermManageShares, 204, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			inv, _ := e.pendingFor(t, tk.ID, "p-"+e.callerEmail, task.RoleViewer)
			return "DELETE", taskPath(tk.ID, "/invitations/"+inv.ID.String()), nil
		}},
		{"change member role", task.PermManageShares, 200, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			m := e.verifiedUser(t)
			e.addMember(t, tk.ID, m.ID, m.Email, task.RoleViewer)
			return "PATCH", taskPath(tk.ID, "/members/"+m.ID.String()), map[string]any{"role": "editor"}
		}},
		{"remove member", task.PermManageShares, 204, func(t *testing.T, e *env, tk task.Task) (string, string, any) {
			t.Helper()
			m := e.verifiedUser(t)
			e.addMember(t, tk.ID, m.ID, m.Email, task.RoleViewer)
			return "DELETE", taskPath(tk.ID, "/members/"+m.ID.String()), nil
		}},
	}

	for _, r := range rows {
		for _, role := range roles {
			t.Run(fmt.Sprintf("%s/%s", r.name, role), func(t *testing.T) {
				t.Parallel()
				e := newEnv(t)
				var tk task.Task
				if role == task.RoleOwner {
					tk = ownedBy(t, e)
				} else {
					tk, _ = e.asMember(t, role)
				}
				method, path, body := r.build(t, e, tk)
				st, msg := send(t, e, method, path, body)
				if role.Can(r.perm) {
					if st != r.wantOK {
						t.Errorf("status = %d (%s), want %d: the role has the permission", st, strings.TrimSpace(msg), r.wantOK)
					}
				} else if st != http.StatusForbidden {
					t.Errorf("status = %d (%s), want 403: the role lacks the permission", st, strings.TrimSpace(msg))
				}
			})
		}
	}
}

// TestPatchMissingBothPermissions: a PATCH that needs both permissions and has
// neither reports the edit-content error and counts the denial once, under
// edit_content.
func TestPatchMissingBothPermissions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	tk, _ := e.asMember(t, task.RoleViewer)
	st, msg := send(t, e, "PATCH", taskPath(tk.ID, ""), map[string]any{"title": "x", "status": "done"})
	if st != http.StatusForbidden || !strings.Contains(msg, "cannot edit") {
		t.Fatalf("response = %d %s, want 403 with the edit-content message", st, msg)
	}
	if got := e.denied(t, "edit_content"); got != 1 {
		t.Errorf("edit_content = %v, want 1", got)
	}
	if got := e.denied(t, "change_status"); got != 0 {
		t.Errorf("change_status = %v, want 0", got)
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
