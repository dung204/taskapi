package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

func taskPath(id uuid.UUID, rest string) string { return "/tasks/" + id.String() + rest }

// stranger returns a task of another user, which the caller cannot see.
func stranger(t *testing.T, e *env) task.Task {
	t.Helper()
	return e.seedTask(t, e.newUser(t).ID, nil)
}

// ownedBy returns a new task of the caller.
func ownedBy(t *testing.T, e *env) task.Task {
	t.Helper()
	return e.seedTask(t, e.caller, nil)
}

func jsonBody(t *testing.T, body []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("body %q is not JSON: %v", body, err)
	}
	return v
}

// explicitCases are the cases of each operation that are not shared with the others.
func explicitCases() []httpCase {
	return []httpCase{
		// --- tasks ---
		{op: "createTask", status: 201, setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: "/tasks", body: map[string]any{"title": "new"}}
		}},
		{op: "createTask", status: 400, example: "titleLength", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: "/tasks", body: map[string]any{"title": ""}}
		}},
		{op: "listTasks", status: 200, setup: func(t *testing.T, e *env) call {
			t.Helper()
			ownedBy(t, e)
			return call{method: "GET", path: "/tasks"}
		}, check: func(t *testing.T, e *env, body []byte) {
			t.Helper()
			if got, ok := jsonBody(t, body).([]any); !ok || len(got) != 1 {
				t.Errorf("body = %s, want a JSON array with the caller's task", body)
			}
		}},
		{op: "listTasks", status: 400, example: "limitNotInteger", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "GET", path: "/tasks?limit=abc"}
		}},
		{op: "getTask", status: 200, setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "GET", path: taskPath(ownedBy(t, e).ID, "")}
		}},
		{op: "getTask", status: 400, example: "invalidId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "GET", path: "/tasks/abc"}
		}},
		{op: "getTask", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "GET", path: taskPath(stranger(t, e).ID, "")}
		}},
		{op: "updateTask", status: 200, setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PATCH", path: taskPath(ownedBy(t, e).ID, ""), body: map[string]any{"title": "changed"}}
		}},
		{op: "updateTask", status: 400, example: "titleLength", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PATCH", path: taskPath(ownedBy(t, e).ID, ""), body: map[string]any{"title": ""}}
		}},
		{op: "updateTask", status: 403, example: "cannotEditContent", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleViewer)
			return call{method: "PATCH", path: taskPath(tk.ID, ""), body: map[string]any{"title": "x"}}
		}},
		{op: "updateTask", status: 403, example: "cannotChangeStatus", name: "", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleViewer)
			return call{method: "PATCH", path: taskPath(tk.ID, ""), body: map[string]any{"status": "done"}}
		}},
		{op: "updateTask", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PATCH", path: taskPath(stranger(t, e).ID, ""), body: map[string]any{"title": "x"}}
		}},
		{op: "deleteTask", status: 204, setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(ownedBy(t, e).ID, "")}
		}},
		{op: "deleteTask", status: 400, example: "invalidId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: "/tasks/abc"}
		}},
		{op: "deleteTask", status: 403, example: "ownerOnly", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleManager)
			return call{method: "DELETE", path: taskPath(tk.ID, "")}
		}},
		{op: "deleteTask", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(stranger(t, e).ID, "")}
		}},

		// --- assignee ---
		{op: "setAssignee", status: 200, setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PUT", path: taskPath(ownedBy(t, e).ID, "/assignee"), body: map[string]any{"email": e.callerEmail}}
		}},
		{op: "setAssignee", status: 400, example: "invalidEmail", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PUT", path: taskPath(ownedBy(t, e).ID, "/assignee"), body: map[string]any{"email": "nope"}}
		}},
		{op: "setAssignee", status: 403, example: "cannotAssign", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleEditor)
			return call{method: "PUT", path: taskPath(tk.ID, "/assignee"), body: map[string]any{"email": e.callerEmail}}
		}},
		{op: "setAssignee", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PUT", path: taskPath(stranger(t, e).ID, "/assignee"), body: map[string]any{"email": e.callerEmail}}
		}},
		{op: "setAssignee", status: 422, example: "notOwnerOrMember", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PUT", path: taskPath(ownedBy(t, e).ID, "/assignee"), body: map[string]any{"email": e.verifiedUser(t).Email}}
		}},
		{op: "clearAssignee", status: 204, setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(ownedBy(t, e).ID, "/assignee")}
		}},
		{op: "clearAssignee", status: 400, example: "invalidId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: "/tasks/abc/assignee"}
		}},
		{op: "clearAssignee", status: 403, example: "cannotAssign", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleViewer)
			return call{method: "DELETE", path: taskPath(tk.ID, "/assignee")}
		}},
		{op: "clearAssignee", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(stranger(t, e).ID, "/assignee")}
		}},

		// --- shares and invitations ---
		{op: "listShares", status: 200, name: "-shape", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := ownedBy(t, e)
			e.addMember(t, tk.ID, e.verifiedUser(t).ID, "m-"+e.callerEmail, task.RoleEditor)
			e.pendingFor(t, tk.ID, "p-"+e.callerEmail, task.RoleViewer)
			return call{method: "GET", path: taskPath(tk.ID, "/shares")}
		}, check: func(t *testing.T, e *env, body []byte) {
			t.Helper()
			list, _ := jsonBody(t, body).([]any)
			if len(list) != 2 {
				t.Fatalf("body = %s, want two shares", body)
			}
			for _, item := range list {
				m := item.(map[string]any)
				for _, k := range []string{"kind", "id", "email", "role", "created_at"} {
					if _, ok := m[k]; !ok {
						t.Errorf("share %v lacks %q", m, k)
					}
				}
				_, hasExpiry := m["expires_at"]
				if (m["kind"] == "invitation") != hasExpiry {
					t.Errorf("share %v: expires_at must be present exactly for an invitation", m)
				}
			}
		}},
		{op: "listShares", status: 200, name: "-empty", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "GET", path: taskPath(ownedBy(t, e).ID, "/shares")}
		}, check: func(t *testing.T, e *env, body []byte) {
			t.Helper()
			if strings.TrimSpace(string(body)) != "[]" {
				t.Errorf("body = %s, want [] and not null", body)
			}
		}},
		{op: "listShares", status: 400, example: "invalidId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "GET", path: "/tasks/abc/shares"}
		}},
		{op: "listShares", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "GET", path: taskPath(stranger(t, e).ID, "/shares")}
		}},

		{op: "inviteToTask", status: 201, setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: taskPath(ownedBy(t, e).ID, "/invitations"), body: map[string]any{"email": "new-" + e.callerEmail, "role": "viewer"}}
		}},
		{op: "inviteToTask", status: 400, example: "invalidEmail", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: taskPath(ownedBy(t, e).ID, "/invitations"), body: map[string]any{"email": "nope", "role": "viewer"}}
		}},
		{op: "inviteToTask", status: 400, example: "bodyMalformedJson", name: "-body", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: taskPath(ownedBy(t, e).ID, "/invitations"), body: `{"email":`}
		}},
		{op: "inviteToTask", status: 403, example: "ownerOnly", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleManager)
			return call{method: "POST", path: taskPath(tk.ID, "/invitations"), body: map[string]any{"email": "x@example.com", "role": "viewer"}}
		}},
		{op: "inviteToTask", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: taskPath(stranger(t, e).ID, "/invitations"), body: map[string]any{"email": "x@example.com", "role": "viewer"}}
		}},
		{op: "inviteToTask", status: 409, example: "alreadyInvited", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := ownedBy(t, e)
			mail := "late-" + e.callerEmail
			e.pendingFor(t, tk.ID, mail, task.RoleViewer)
			return call{method: "POST", path: taskPath(tk.ID, "/invitations"), body: map[string]any{"email": mail, "role": "viewer"}}
		}},
		{op: "inviteToTask", status: 409, example: "alreadyMember", name: "-member", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := ownedBy(t, e)
			m := e.verifiedUser(t)
			e.addMember(t, tk.ID, m.ID, m.Email, task.RoleViewer)
			return call{method: "POST", path: taskPath(tk.ID, "/invitations"), body: map[string]any{"email": m.Email, "role": "viewer"}}
		}},
		{op: "inviteToTask", status: 422, example: "invalidRole", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: taskPath(ownedBy(t, e).ID, "/invitations"), body: map[string]any{"email": "x@example.com", "role": "owner"}}
		}},
		{op: "inviteToTask", status: 422, example: "ownEmail", name: "-self", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: taskPath(ownedBy(t, e).ID, "/invitations"), body: map[string]any{"email": e.callerEmail, "role": "viewer"}}
		}},
		{op: "inviteToTask", status: 429, example: "tooManyRequests", name: "-owner-hourly-cap", setup: func(t *testing.T, e *env) call {
			t.Helper()
			e.ownerLimit.retryAfter = 90 * time.Second
			return call{method: "POST", path: taskPath(ownedBy(t, e).ID, "/invitations"), body: map[string]any{"email": "x@example.com", "role": "viewer"},
				wantHeaders: map[string]string{"Retry-After": "90"}}
		}},

		{op: "cancelInvitation", status: 204, setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := ownedBy(t, e)
			inv, _ := e.pendingFor(t, tk.ID, "x-"+e.callerEmail, task.RoleViewer)
			return call{method: "DELETE", path: taskPath(tk.ID, "/invitations/"+inv.ID.String())}
		}},
		{op: "cancelInvitation", status: 400, example: "invalidInvitationId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(ownedBy(t, e).ID, "/invitations/abc")}
		}},
		{op: "cancelInvitation", status: 403, example: "ownerOnly", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleManager)
			return call{method: "DELETE", path: taskPath(tk.ID, "/invitations/"+uuid.NewV7().String())}
		}},
		{op: "cancelInvitation", status: 404, example: "invitationNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(ownedBy(t, e).ID, "/invitations/"+uuid.NewV7().String())}
		}},
		{op: "cancelInvitation", status: 404, example: "taskNotFound", name: "-task", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(stranger(t, e).ID, "/invitations/"+uuid.NewV7().String())}
		}},

		{op: "leaveTask", status: 204, setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleViewer)
			return call{method: "DELETE", path: taskPath(tk.ID, "/members/me")}
		}},
		{op: "leaveTask", status: 400, example: "invalidId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: "/tasks/abc/members/me"}
		}},
		{op: "leaveTask", status: 404, example: "taskNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(stranger(t, e).ID, "/members/me")}
		}},
		{op: "leaveTask", status: 404, example: "ownerCannotLeave", name: "-owner", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(ownedBy(t, e).ID, "/members/me")}
		}},

		{op: "changeMemberRole", status: 200, setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := ownedBy(t, e)
			m := e.verifiedUser(t)
			e.addMember(t, tk.ID, m.ID, m.Email, task.RoleViewer)
			return call{method: "PATCH", path: taskPath(tk.ID, "/members/"+m.ID.String()), body: map[string]any{"role": "editor"}}
		}},
		{op: "changeMemberRole", status: 400, example: "invalidUserId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PATCH", path: taskPath(ownedBy(t, e).ID, "/members/abc"), body: map[string]any{"role": "editor"}}
		}},
		{op: "changeMemberRole", status: 403, example: "ownerOnly", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleManager)
			return call{method: "PATCH", path: taskPath(tk.ID, "/members/"+uuid.NewV7().String()), body: map[string]any{"role": "editor"}}
		}},
		{op: "changeMemberRole", status: 404, example: "memberNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PATCH", path: taskPath(ownedBy(t, e).ID, "/members/"+uuid.NewV7().String()), body: map[string]any{"role": "editor"}}
		}},
		{op: "changeMemberRole", status: 422, example: "invalidRole", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "PATCH", path: taskPath(ownedBy(t, e).ID, "/members/"+uuid.NewV7().String()), body: map[string]any{"role": "owner"}}
		}},

		{op: "removeMember", status: 204, setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := ownedBy(t, e)
			m := e.verifiedUser(t)
			e.addMember(t, tk.ID, m.ID, m.Email, task.RoleViewer)
			return call{method: "DELETE", path: taskPath(tk.ID, "/members/"+m.ID.String())}
		}},
		{op: "removeMember", status: 400, example: "invalidUserId", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(ownedBy(t, e).ID, "/members/abc")}
		}},
		{op: "removeMember", status: 403, example: "ownerOnly", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk, _ := e.asMember(t, task.RoleManager)
			return call{method: "DELETE", path: taskPath(tk.ID, "/members/"+uuid.NewV7().String())}
		}},
		{op: "removeMember", status: 404, example: "memberNotFound", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "DELETE", path: taskPath(ownedBy(t, e).ID, "/members/"+uuid.NewV7().String())}
		}},

		{op: "acceptInvitation", status: 200, setup: func(t *testing.T, e *env) call {
			t.Helper()
			owner := e.verifiedUser(t)
			tk := e.seedTask(t, owner.ID, nil)
			_, token := e.pendingFor(t, tk.ID, e.callerEmail, task.RoleEditor)
			return call{method: "POST", path: "/invitations/accept", body: map[string]any{"token": token}}
		}, check: func(t *testing.T, e *env, body []byte) {
			t.Helper()
			if m, _ := jsonBody(t, body).(map[string]any); m["role"] != "editor" {
				t.Errorf("role = %v, want editor", m["role"])
			}
		}},
		{op: "acceptInvitation", status: 400, example: "tokenMalformed", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: "/invitations/accept", body: map[string]any{"token": "short"}}
		}},
		{op: "acceptInvitation", status: 404, example: "invitationNotFoundOrExpired", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: "/invitations/accept", body: map[string]any{"token": "A2E7Iw6mKZ0ZB3V9QJxqk0cL8wH4o1nYd5rTgS_UfXc"}}
		}},
		{op: "acceptInvitation", status: 409, example: "memberLimit", setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := e.seedTask(t, e.verifiedUser(t).ID, nil)
			for range 50 {
				m := e.verifiedUser(t)
				e.addMember(t, tk.ID, m.ID, m.Email, task.RoleViewer)
			}
			_, token := e.pendingFor(t, tk.ID, e.callerEmail, task.RoleViewer)
			return call{method: "POST", path: "/invitations/accept", body: map[string]any{"token": token}}
		}},

		{op: "declineInvitation", status: 204, setup: func(t *testing.T, e *env) call {
			t.Helper()
			tk := e.seedTask(t, e.verifiedUser(t).ID, nil)
			_, token := e.pendingFor(t, tk.ID, e.callerEmail, task.RoleViewer)
			return call{method: "POST", path: "/invitations/decline", body: map[string]any{"token": token}}
		}},
		{op: "declineInvitation", status: 400, example: "tokenMalformed", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: "/invitations/decline", body: map[string]any{"token": "short"}}
		}},
		{op: "declineInvitation", status: 404, example: "invitationNotFoundOrExpired", setup: func(t *testing.T, e *env) call {
			t.Helper()
			return call{method: "POST", path: "/invitations/decline", body: map[string]any{"token": "A2E7Iw6mKZ0ZB3V9QJxqk0cL8wH4o1nYd5rTgS_UfXc"}}
		}},
	}
}
