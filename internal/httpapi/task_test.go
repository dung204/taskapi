package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil/logtest"
)

// idValid in a table stands for the ID of the task the test seeded for the
// caller; the tests use fresh IDs, so a table cannot hold them.
const idValid = "valid"

var notFoundID = uuid.MustParse("01a142fd-186e-7169-95c2-a186f9103a97")

// resolveID replaces idValid by the ID of tk.
func resolveID(id string, tk task.Task) string {
	if id == idValid {
		return tk.ID.String()
	}
	return id
}

// ownerAPITask is how the API shows tk, after mod, to its owner.
func ownerAPITask(tk task.Task, mod func(*task.Task)) oapigen.Task {
	if mod != nil {
		mod(&tk)
	}
	return oapigen.ToAPITask(task.Access{Task: tk, Role: task.RoleOwner})
}

func checkAPITask(tb testing.TB, got, want oapigen.Task, opts ...cmp.Option) {
	tb.Helper()
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("task mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateTask(t *testing.T) {
	t.Parallel()
	dueAt := time.Date(2026, 10, 14, 9, 53, 50, 0, time.UTC).Truncate(time.Microsecond)
	dueAtStr := dueAt.Format(time.RFC3339)

	tests := []struct {
		name            string
		body            map[string]any
		wantStatusCode  int
		wantTask        *oapigen.Task
		wantErrContains []string
	}{
		{
			name: "success",
			body: map[string]any{
				"title":       "Task title",
				"description": "Task description",
				"status":      "todo",
				"due_at":      dueAtStr,
			},
			wantStatusCode: http.StatusCreated,
			wantTask: &oapigen.Task{
				Title:       "Task title",
				Description: "Task description",
				Status:      "todo",
				DueAt:       &dueAt,
				Role:        task.RoleOwner,
			},
		},
		{
			name: "invalid due_at",
			body: map[string]any{
				"title":       "Task title",
				"description": "Task description",
				"status":      "todo",
				"due_at":      "aaa",
			},
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"due_at", "date-time", "RFC3339"},
		},
		{
			name: "invalid title",
			body: map[string]any{
				"title": "",
			},
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"title", "at least 1", "at most 200"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, e, _ := setupTest(t)
			rec, req := prepareTestRequest(t, http.MethodPost, "/tasks", tt.body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if tt.wantTask != nil {
				got := decodeResponseBody[oapigen.Task](t, res.Body)
				checkAPITask(t, got, *tt.wantTask, cmpopts.IgnoreFields(oapigen.Task{}, "Id", "OwnerId", "CreatedAt", "UpdatedAt"))
				if got.OwnerId != e.caller {
					t.Errorf("owner = %v, want the caller %v", got.OwnerId, e.caller)
				}
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

// captureListStore records the filter and delegates to the real store.
type captureListStore struct {
	task.Store
	got task.ListFilter
}

func (s *captureListStore) List(ctx context.Context, viewer uuid.UUID, f task.ListFilter) ([]task.Access, error) {
	s.got = f
	return s.Store.List(ctx, viewer, f)
}

func defaultListFilter() task.ListFilter {
	return task.ListFilter{
		Limit:  20,
		Offset: 0,
	}
}

func listFilterWith(mod func(*task.ListFilter)) task.ListFilter {
	f := defaultListFilter()
	mod(&f)
	return f
}

func checkListFilter(t *testing.T, got task.ListFilter, want task.ListFilter) {
	t.Helper()
	if got.Limit != want.Limit {
		t.Errorf("limit = %d, want %d", got.Limit, want.Limit)
	}
	if got.Offset != want.Offset {
		t.Errorf("offset = %d, want %d", got.Offset, want.Offset)
	}
	switch {
	case got.Status == nil && want.Status == nil:
	case got.Status == nil || want.Status == nil:
		t.Errorf("status = %v, want %v", got.Status, want.Status)
	case *got.Status != *want.Status:
		t.Errorf("status = %q, want %q", *got.Status, *want.Status)
	}
}

func TestListTasks_Filter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		params          string
		wantStatusCode  int
		wantFilter      task.ListFilter
		wantErrContains []string
	}{
		{
			name:           "defaults",
			wantStatusCode: http.StatusOK,
			wantFilter:     defaultListFilter(),
		},
		{
			name:           "valid filter",
			params:         "?limit=5&offset=10&status=done",
			wantStatusCode: http.StatusOK,
			wantFilter: task.ListFilter{
				Limit:  5,
				Offset: 10,
				Status: new(task.StatusDone),
			},
		},
		{
			name:           "big limit",
			params:         "?limit=100",
			wantStatusCode: http.StatusOK,
			wantFilter:     listFilterWith(func(lf *task.ListFilter) { lf.Limit = 100 }),
		},
		{
			name:           "limit exceeds 100",
			params:         "?limit=101",
			wantStatusCode: http.StatusOK,
			wantFilter:     listFilterWith(func(lf *task.ListFilter) { lf.Limit = 100 }),
		},
		{
			name:            "non-numerical limit",
			params:          "?limit=abc",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"limit", "invalid"},
		},
		{
			name:            "zero limit",
			params:          "?limit=0",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"limit", "invalid"},
		},
		{
			name:            "negative limit",
			params:          "?limit=-1",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"limit", "invalid"},
		},
		{
			name:            "negative offset",
			params:          "?offset=-1",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"offset", "invalid"},
		},
		{
			name:            "non-numerical offset",
			params:          "?offset=abc",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"offset", "invalid"},
		},
		{
			name:           "status overdue",
			params:         "?status=overdue",
			wantStatusCode: http.StatusOK,
			wantFilter:     listFilterWith(func(lf *task.ListFilter) { lf.Status = new(task.StatusOverdue) }),
		},
		{
			name:            "invalid status",
			params:          "?status=invalid",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"status", "invalid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			store := &captureListStore{Store: e.tasks}
			h, _, _ := setupTestWith(t, e, store)
			path := "/tasks" + tt.params
			rec, req := prepareTestRequest(t, http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
				return
			}

			checkListFilter(t, store.got, tt.wantFilter)
		})
	}
}

// seedTasks stores n tasks for owner whose IDs sort in creation order, and
// returns them (index 0 is the first). The IDs share a random prefix, so
// tests of one package never collide.
func seedTasks(t *testing.T, e *env, owner uuid.UUID, n int) []uuid.UUID {
	t.Helper()
	prefix := rand.Uint32()
	ids := make([]uuid.UUID, 0, n)
	for i := 1; i <= n; i++ {
		status := task.StatusTodo
		if i%2 == 0 {
			status = task.StatusDone
		}
		id := uuid.MustParse(fmt.Sprintf("%08x-0000-7000-8000-%012d", prefix, i))
		_, err := e.tasks.Create(t.Context(), task.Task{
			ID:      id,
			OwnerID: owner,
			Title:   fmt.Sprintf("task %d", i),
			Status:  status,
		})
		if err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestListTasks_Paging(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		seed  int
		query string
		want  []int // 1-based creation order of the tasks expected, in response order
	}{
		{"no tasks", 0, "", nil},
		{"default limit returns all", 5, "", []int{5, 4, 3, 2, 1}},
		{"first page", 5, "?limit=2", []int{5, 4}},
		{"second page", 5, "?limit=2&offset=2", []int{3, 2}},
		{"last partial page", 5, "?limit=2&offset=4", []int{1}},
		{"offset equals count", 5, "?limit=2&offset=5", nil},
		{"offset exceeds count", 5, "?limit=2&offset=100", nil},
		{"limit exceeds count", 5, "?limit=10", []int{5, 4, 3, 2, 1}},
		{"status filter then page", 5, "?status=done&limit=1&offset=1", []int{2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, e, _ := setupTest(t)
			seeded := seedTasks(t, e, e.caller, tt.seed)
			wantIDs := make([]uuid.UUID, 0, len(tt.want))
			for _, n := range tt.want {
				wantIDs = append(wantIDs, seeded[n-1])
			}
			path := "/tasks" + tt.query
			rec, req := prepareTestRequest(t, http.MethodGet, path, nil)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusOK)
			}

			gotTasks := decodeResponseBody[[]oapigen.Task](t, res.Body)

			// An empty page must be encoded as [] rather than null:
			// [] decodes to an empty non-nil slice, while null decodes to nil.
			if len(wantIDs) == 0 && gotTasks == nil {
				t.Fatalf("body decoded to a nil slice, want an empty array [] (not null)")
			}

			gotIDs := make([]uuid.UUID, 0)
			for _, tsk := range gotTasks {
				gotIDs = append(gotIDs, tsk.Id)
			}

			if !slices.Equal(gotIDs, wantIDs) {
				t.Errorf("ids = %v, want %v", gotIDs, wantIDs)
			}
		})
	}
}

func TestGetTask(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		id              string
		wantStatusCode  int
		wantTask        bool
		wantErrContains []string
	}{
		{
			name:            "invalid UUID",
			id:              "abc",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"invalid"},
		},
		{
			name:            "valid UUID but not found",
			id:              notFoundID.String(),
			wantStatusCode:  http.StatusNotFound,
			wantErrContains: []string{"not found"},
		},
		{
			name:           "valid UUID and found",
			id:             idValid,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, e, _ := setupTest(t)
			seeded := e.seedTask(t, e.caller, nil)
			rec, req := prepareTestRequest(t, http.MethodGet, `/tasks/`+resolveID(tt.id, seeded), nil)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if tt.wantTask {
				checkAPITask(t, decodeResponseBody[oapigen.Task](t, res.Body), ownerAPITask(seeded, nil))
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

func TestUpdateTask(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		id              string
		body            string
		wantStatusCode  int
		wantMod         func(*task.Task) // what the update changed
		wantTask        bool
		wantErrContains []string
	}{
		{
			name:            "invalid ID",
			id:              "abc",
			body:            "{}",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"invalid"},
		},
		{
			name:            "empty body",
			id:              idValid,
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"required"},
		},
		{
			name:            "malformed body",
			id:              idValid,
			body:            `{"title":"abc"`,
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"malformed"},
		},
		{
			name:            "wrong field type",
			id:              idValid,
			body:            `{"title":123}`,
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"wrong type"},
		},
		{
			name:            "non-object body",
			id:              idValid,
			body:            `[]`,
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"JSON object"},
		},
		{
			name:            "invalid due_at",
			id:              idValid,
			body:            `{"due_at":"tomorrow"}`,
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"RFC3339"},
		},
		{
			name:           "partial update",
			id:             idValid,
			body:           `{"title":"new"}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
			wantMod:        func(t *task.Task) { t.Title = "new" },
		},
		{
			name:           "set description to blank",
			id:             idValid,
			body:           `{"description":""}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
			wantMod:        func(t *task.Task) { t.Description = "" },
		},
		{
			name:           "valid due_at in UTC",
			id:             idValid,
			body:           `{"due_at":"2026-12-31T10:00:00Z"}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
			wantMod: func(t *task.Task) {
				t.DueAt = new(time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC))
			},
		},
		{
			// 10:00 at +07:00 is 03:00 UTC
			name:           "valid due_at with timezone offset",
			id:             idValid,
			body:           `{"due_at":"2026-12-31T10:00:00+07:00"}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
			wantMod: func(t *task.Task) {
				t.DueAt = new(time.Date(2026, 12, 31, 3, 0, 0, 0, time.UTC))
			},
		},
		{
			name:           "valid status todo",
			id:             idValid,
			body:           `{"status":"todo"}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
			wantMod:        func(t *task.Task) { t.Status = task.StatusTodo },
		},
		{
			name:           "valid status doing",
			id:             idValid,
			body:           `{"status":"doing"}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
			wantMod:        func(t *task.Task) { t.Status = task.StatusDoing },
		},
		{
			name:           "valid status done",
			id:             idValid,
			body:           `{"status":"done"}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
			wantMod:        func(t *task.Task) { t.Status = task.StatusDone },
		},
		{
			name:           "empty object leaves unchanged",
			id:             idValid,
			body:           `{}`,
			wantStatusCode: http.StatusOK,
			wantTask:       true,
		},
		{
			name:            "valid id but not found",
			id:              notFoundID.String(),
			body:            `{}`,
			wantStatusCode:  http.StatusNotFound,
			wantErrContains: []string{"not found"},
		},
		{
			name:            "invalid title",
			id:              idValid,
			body:            `{"title":""}`,
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"title", "at least 1", "at most 200", "characters"},
		},
		{
			name:            "invalid status",
			id:              idValid,
			body:            `{"status":"bogus"}`,
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"status", "invalid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, e, _ := setupTest(t)
			seeded := e.seedTask(t, e.caller, nil)
			rec, req := prepareTestRequest(t, http.MethodPatch, "/tasks/"+resolveID(tt.id, seeded), tt.body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if tt.wantTask {
				gotTask := decodeResponseBody[oapigen.Task](t, res.Body)
				// We don't focus on the UpdatedAt field here
				// since it's in a separate suite for Patch() method of Task
				checkAPITask(t, gotTask, ownerAPITask(seeded, tt.wantMod), cmpopts.IgnoreFields(oapigen.Task{}, "UpdatedAt"))
			}

			// A rejected request must leave the stored task alone.
			if tt.wantStatusCode >= 400 {
				stored, err := e.tasks.Get(t.Context(), e.caller, seeded.ID)
				if err != nil {
					t.Fatalf("get stored task: %v", err)
				}
				if diff := cmp.Diff(seeded, stored.Task); diff != "" {
					t.Errorf("task changed by a rejected request (-seeded +stored):\n%s", diff)
				}
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

func TestDeleteTask(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		id              string
		wantStatusCode  int
		wantErrContains []string
	}{
		{
			name:            "invalid ID",
			id:              "abc",
			wantStatusCode:  http.StatusBadRequest,
			wantErrContains: []string{"invalid"},
		},
		{
			name:            "valid ID but not found",
			id:              notFoundID.String(),
			wantStatusCode:  http.StatusNotFound,
			wantErrContains: []string{"not found"},
		},
		{
			name:           "success",
			id:             idValid,
			wantStatusCode: http.StatusNoContent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, e, _ := setupTest(t)
			seeded := e.seedTask(t, e.caller, nil)
			rec, req := prepareTestRequest(t, http.MethodDelete, "/tasks/"+resolveID(tt.id, seeded), nil)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			_, err := e.tasks.Get(t.Context(), e.caller, seeded.ID)
			if res.StatusCode == http.StatusNoContent {
				if !errors.Is(err, task.ErrNotFound) {
					t.Fatalf("Get after deletion = %v, want ErrNotFound", err)
				}
			} else if err != nil {
				t.Fatalf("a rejected request must keep the task: %v", err)
			}

			if res.StatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

func TestTaskRolePermissions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		role            task.Role
		method          string
		body            any
		wantStatusCode  int
		wantWritten     bool // the store was updated or deleted
		wantRole        task.Role
		wantErrContains []string
	}{
		{name: "viewer gets the task", role: task.RoleViewer, method: http.MethodGet, wantStatusCode: http.StatusOK, wantRole: task.RoleViewer},
		{name: "editor gets the task", role: task.RoleEditor, method: http.MethodGet, wantStatusCode: http.StatusOK, wantRole: task.RoleEditor},

		{
			name: "viewer cannot edit content", role: task.RoleViewer, method: http.MethodPatch,
			body: `{"title":"new"}`, wantStatusCode: http.StatusForbidden, wantErrContains: []string{"cannot edit"},
		},
		{
			name: "viewer cannot change status", role: task.RoleViewer, method: http.MethodPatch,
			body: `{"status":"done"}`, wantStatusCode: http.StatusForbidden, wantErrContains: []string{"cannot change the status"},
		},
		{
			name: "contributor cannot edit content", role: task.RoleContributor, method: http.MethodPatch,
			body: `{"description":"new"}`, wantStatusCode: http.StatusForbidden, wantErrContains: []string{"cannot edit"},
		},
		{
			name: "contributor can change status", role: task.RoleContributor, method: http.MethodPatch,
			body: `{"status":"done"}`, wantStatusCode: http.StatusOK, wantWritten: true, wantRole: task.RoleContributor,
		},
		{
			name: "editor can edit content", role: task.RoleEditor, method: http.MethodPatch,
			body: `{"title":"new"}`, wantStatusCode: http.StatusOK, wantWritten: true, wantRole: task.RoleEditor,
		},
		{
			// One forbidden field rejects the whole patch.
			name: "contributor patching content and status is rejected", role: task.RoleContributor, method: http.MethodPatch,
			body: `{"title":"new","status":"done"}`, wantStatusCode: http.StatusForbidden, wantErrContains: []string{"cannot edit"},
		},

		{
			name: "viewer cannot delete", role: task.RoleViewer, method: http.MethodDelete,
			wantStatusCode: http.StatusForbidden, wantErrContains: []string{"only the owner"},
		},
		{
			name: "manager cannot delete", role: task.RoleManager, method: http.MethodDelete,
			wantStatusCode: http.StatusForbidden, wantErrContains: []string{"only the owner"},
		},
		{name: "owner can delete", role: task.RoleOwner, method: http.MethodDelete, wantStatusCode: http.StatusNoContent, wantWritten: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			owner := e.newUser(t)
			seeded := e.seedTask(t, owner.ID, nil)
			caller := owner
			if tt.role != task.RoleOwner {
				caller = e.newUser(t)
				e.addMember(t, seeded.ID, caller.ID, caller.Email, tt.role)
			}
			h := withTestAuthAs(t, caller.ID, HandlerDeps{Tasks: e.service(nil), Logger: slog.New(slog.DiscardHandler)})
			rec, req := prepareTestRequest(t, tt.method, "/tasks/"+seeded.ID.String(), tt.body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			stored, err := e.tasks.Get(t.Context(), owner.ID, seeded.ID)
			written := errors.Is(err, task.ErrNotFound) || (err == nil && cmp.Diff(seeded, stored.Task) != "")
			if err != nil && !errors.Is(err, task.ErrNotFound) {
				t.Fatalf("get stored task: %v", err)
			}
			if written != tt.wantWritten {
				t.Errorf("task written = %t, want %t", written, tt.wantWritten)
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
				return
			}

			if tt.wantRole != "" {
				if got := decodeResponseBody[oapigen.Task](t, res.Body).Role; got != tt.wantRole {
					t.Errorf("role = %q, want %q", got, tt.wantRole)
				}
			}
		})
	}
}

// TestTaskOfAnotherUser checks that a task the caller cannot see is a 404 for
// every operation, and that update and delete leave it alone.
func TestTaskOfAnotherUser(t *testing.T) {
	t.Parallel()
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			h, e, _ := setupTest(t)
			other := e.newUser(t)
			seeded := e.seedTask(t, other.ID, nil)

			var body any
			if method == http.MethodPatch {
				body = `{"title":"hijacked"}`
			}

			rec, req := prepareTestRequest(t, method, "/tasks/"+seeded.ID.String(), body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusNotFound)
			}

			got, err := e.tasks.Get(t.Context(), other.ID, seeded.ID)
			if err != nil {
				t.Fatalf("task is gone after %s: %v", method, err)
			}
			if got.Task.Title != seeded.Title {
				t.Errorf("title = %q, want %q (unchanged)", got.Task.Title, seeded.Title)
			}
		})
	}
}

type mockFailingStore struct {
	task.Store
	err error
}

func (s mockFailingStore) Create(ctx context.Context, t task.Task) (task.Task, error) {
	return task.Task{}, s.err
}

func (s mockFailingStore) Delete(ctx context.Context, id uuid.UUID) error {
	return s.err
}

func (s mockFailingStore) Get(ctx context.Context, _ uuid.UUID, id uuid.UUID) (task.Access, error) {
	return task.Access{}, s.err
}

func (s mockFailingStore) List(ctx context.Context, _ uuid.UUID, f task.ListFilter) ([]task.Access, error) {
	return []task.Access{}, s.err
}

func (s mockFailingStore) Update(ctx context.Context, t task.Task) error {
	return s.err
}

func TestHandleStoreError(t *testing.T) {
	t.Parallel()
	storeEndpoints := []struct {
		name, method, path string
		body               any
	}{
		{"create", http.MethodPost, "/tasks", map[string]any{"title": "x"}},
		{"list", http.MethodGet, "/tasks", nil},
		{"get one", http.MethodGet, "/tasks/" + notFoundID.String(), nil},
		{"update", http.MethodPatch, "/tasks/" + notFoundID.String(), map[string]any{"title": "y"}},
		{"delete", http.MethodDelete, "/tasks/" + notFoundID.String(), nil},
	}

	errUnavailable := fmt.Errorf("get task: %w", apperr.ErrServiceUnavailable)
	errGeneric := errors.New("oops")

	errCases := []struct {
		name            string
		err             error
		wantStatusCode  int
		wantErrContains []string
		wantHidden      []string
		wantEmptyBody   bool            // the handler writes nothing (client went away)
		wantLogs        []logtest.Entry // records logged by handleStoreError, before "request completed"
	}{
		{
			name:            "generic error",
			err:             errGeneric,
			wantStatusCode:  http.StatusInternalServerError,
			wantErrContains: []string{"internal"},
			wantHidden:      []string{"oops"},
			wantLogs:        []logtest.Entry{{Level: slog.LevelError, Msg: "unexpected error", Attrs: map[string]any{"error": errGeneric}}},
		},
		{
			name:            "not found",
			err:             apperr.ErrNotFound,
			wantStatusCode:  http.StatusNotFound,
			wantErrContains: []string{"not found"},
		},
		{
			name:            "wrapped not found",
			err:             fmt.Errorf("get thing: %w", apperr.ErrNotFound),
			wantStatusCode:  http.StatusNotFound,
			wantErrContains: []string{"not found"},
		},
		{
			// auth.ErrSessionNotFound is for the auth service's own logic and
			// deliberately does not wrap apperr.ErrNotFound, so it must not become a 404.
			name:            "auth session not found is not a 404",
			err:             auth.ErrSessionNotFound,
			wantStatusCode:  http.StatusInternalServerError,
			wantErrContains: []string{"internal"},
			wantHidden:      []string{"session not found"},
			wantLogs:        []logtest.Entry{{Level: slog.LevelError, Msg: "unexpected error", Attrs: map[string]any{"error": auth.ErrSessionNotFound}}},
		},
		{
			name:            "service unavailable",
			err:             apperr.ErrServiceUnavailable,
			wantStatusCode:  http.StatusServiceUnavailable,
			wantErrContains: []string{"service unavailable"},
			wantLogs:        []logtest.Entry{{Level: slog.LevelError, Msg: "unexpected error", Attrs: map[string]any{"error": apperr.ErrServiceUnavailable}}},
		},
		{
			name:            "wrapped service unavailable",
			err:             errUnavailable,
			wantStatusCode:  http.StatusServiceUnavailable,
			wantErrContains: []string{"service unavailable"},
			wantHidden:      []string{"get task"},
			wantLogs:        []logtest.Entry{{Level: slog.LevelError, Msg: "unexpected error", Attrs: map[string]any{"error": errUnavailable}}},
		},
		{
			name:            "deadline exceeded",
			err:             context.DeadlineExceeded,
			wantStatusCode:  http.StatusServiceUnavailable,
			wantErrContains: []string{"service unavailable"},
		},
		{
			name:            "wrapped deadline",
			err:             fmt.Errorf("get task: %w", context.DeadlineExceeded),
			wantStatusCode:  http.StatusServiceUnavailable,
			wantErrContains: []string{"service unavailable"},
			wantHidden:      []string{"get task"},
		},
		{
			name:           "context canceled",
			err:            context.Canceled,
			wantStatusCode: http.StatusOK, // nothing is written, so the default status stays
			wantEmptyBody:  true,
		},
		{
			name:           "wrapped context canceled",
			err:            fmt.Errorf("get task: %w", context.Canceled),
			wantStatusCode: http.StatusOK,
			wantEmptyBody:  true,
		},
	}

	ignoreAttrs := []string{"duration_ms", "request_id", "method", "path", "stack"}

	for _, ec := range errCases {
		for _, ep := range storeEndpoints {
			name := ep.name + "-" + ec.name
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				h, _, logRec := setupTestWithStore(t, mockFailingStore{err: ec.err})
				rr, req := prepareTestRequest(t, ep.method, ep.path, ep.body)
				h.ServeHTTP(rr, req)

				res := rr.Result()
				defer res.Body.Close()

				if ec.wantStatusCode != res.StatusCode {
					t.Fatalf("status code = %v, want %v", res.StatusCode, ec.wantStatusCode)
				}

				if ec.wantEmptyBody {
					body, err := io.ReadAll(res.Body)
					if err != nil {
						t.Fatalf("read body: %v", err)
					}
					if len(body) != 0 {
						t.Errorf("body = %q, want empty", body)
					}
				} else {
					errMes := getErrorMessageFromResponseBody(t, res.Body)

					checkErrorMesssage(t, errMes, ec.wantErrContains)

					if ec.wantHidden != nil {
						checkErrorMessageHidden(t, errMes, ec.wantHidden)
					}
				}

				// handleStoreError's own records come first, then the access log.
				completedLevel := slog.LevelInfo
				if ec.wantStatusCode >= http.StatusInternalServerError {
					completedLevel = slog.LevelError
				}
				wantLogs := append(slices.Clone(ec.wantLogs), logtest.Entry{
					Level: completedLevel,
					Msg:   "request completed",
					Attrs: map[string]any{"status": int64(ec.wantStatusCode)},
				})
				logtest.CheckEntries(t, logRec.Entries(), wantLogs, logtest.IgnoreAttrs(ignoreAttrs...))
			})
		}
	}
}
