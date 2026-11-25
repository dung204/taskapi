package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil/logtest"
)

var validID = uuid.MustParse("01a142e5-6a3e-7218-9722-c0215445fae0")
var notFoundID = uuid.MustParse("01a142fd-186e-7169-95c2-a186f9103a97")

func defaultTask() task.Task {
	return task.Task{
		ID:          validID,
		OwnerID:     testUserID,
		Title:       "title",
		Description: "description",
		Status:      task.StatusDoing,
		DueAt:       new(time.Date(2026, 10, 14, 9, 53, 50, 0, time.UTC).Truncate(time.Microsecond)),
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
	}
}

func taskWith(mod func(*task.Task)) task.Task {
	t := defaultTask()
	mod(&t)
	return t
}

// ownerAccess is how the store shows t to its owner, the user the tests
// authenticate as.
func ownerAccess(t task.Task) task.Access {
	return task.Access{Task: t, Role: task.RoleOwner}
}

func apiTaskWith(mod func(*task.Task)) oapigen.Task {
	return oapigen.ToAPITask(ownerAccess(taskWith(mod)))
}

func checkAPITask(tb testing.TB, got, want oapigen.Task, opts ...cmp.Option) {
	tb.Helper()
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("task mismatch (-want +got):\n%s", diff)
	}
}

func TestCreateTask(t *testing.T) {
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
				OwnerId:     testUserID,
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
			h, _, _ := setupTest(t)
			rec, req := prepareTestRequest(t, http.MethodPost, "/tasks", tt.body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if tt.wantTask != nil {
				got := decodeResponseBody[oapigen.Task](t, res.Body)
				checkAPITask(t, got, *tt.wantTask, cmpopts.IgnoreFields(oapigen.Task{}, "Id", "CreatedAt", "UpdatedAt"))
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

type mockCaptureListStore struct {
	task.Store
	got   task.ListFilter
	tasks []task.Access
}

func (s *mockCaptureListStore) List(_ context.Context, _ uuid.UUID, f task.ListFilter) ([]task.Access, error) {
	s.got = f
	return s.tasks, nil
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
			store := &mockCaptureListStore{}
			h, _ := setupTestWithStore(t, store)
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

func testID(n int) uuid.UUID {
	return uuid.MustParse(fmt.Sprintf("00000000-0000-7000-8000-%012d", n))
}

func ids(n ...int) []uuid.UUID {
	res := make([]uuid.UUID, 0)
	for _, val := range n {
		res = append(res, testID(val))
	}
	return res
}

func seedTasks(t *testing.T, s task.Store, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		status := task.StatusTodo
		if i%2 == 0 {
			status = task.StatusDone
		}
		_, err := s.Create(t.Context(), task.Task{
			ID:      testID(i),
			OwnerID: testUserID,
			Title:   fmt.Sprintf("task %d", i),
			Status:  status,
		})
		if err != nil {
			t.Fatalf("seed task %d: %v", i, err)
		}
	}
}

func TestListTasks_Paging(t *testing.T) {
	tests := []struct {
		name    string
		seed    int
		query   string
		wantIDs []uuid.UUID
	}{
		{"empty store", 0, "", nil},
		{"default limit returns all", 5, "", ids(5, 4, 3, 2, 1)},
		{"first page", 5, "?limit=2", ids(5, 4)},
		{"second page", 5, "?limit=2&offset=2", ids(3, 2)},
		{"last partial page", 5, "?limit=2&offset=4", ids(1)},
		{"offset equals count", 5, "?limit=2&offset=5", nil},
		{"offset exceeds count", 5, "?limit=2&offset=100", nil},
		{"limit exceeds count", 5, "?limit=10", ids(5, 4, 3, 2, 1)},
		{"status filter then page", 5, "?status=done&limit=1&offset=1", ids(2)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := memory.NewTaskStore()
			seedTasks(t, store, tt.seed)
			h, _ := setupTestWithStore(t, store)
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
			if len(tt.wantIDs) == 0 && gotTasks == nil {
				t.Fatalf("body decoded to a nil slice, want an empty array [] (not null)")
			}

			gotIDs := make([]uuid.UUID, 0)
			for _, tsk := range gotTasks {
				gotIDs = append(gotIDs, tsk.Id)
			}

			if !slices.Equal(gotIDs, tt.wantIDs) {
				t.Errorf("ids = %v, want %v", gotIDs, tt.wantIDs)
			}
		})
	}
}

type mockGetOneStore struct {
	task.Store
	found task.Task
}

func (s mockGetOneStore) Get(_ context.Context, _ uuid.UUID, id uuid.UUID) (task.Access, error) {
	if id == s.found.ID {
		return ownerAccess(s.found), nil
	}

	return task.Access{}, task.ErrNotFound
}

func TestGetTask(t *testing.T) {
	tests := []struct {
		name            string
		id              string
		wantStatusCode  int
		wantTask        *oapigen.Task
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
			id:             validID.String(),
			wantStatusCode: http.StatusOK,
			wantTask:       new(oapigen.ToAPITask(ownerAccess(defaultTask()))),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _ := setupTestWithStore(t, mockGetOneStore{found: defaultTask()})
			rec, req := prepareTestRequest(t, http.MethodGet, `/tasks/`+tt.id, nil)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if tt.wantTask != nil {
				checkAPITask(t, decodeResponseBody[oapigen.Task](t, res.Body), *tt.wantTask)
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

type mockCaptureUpdateStore struct {
	task.Store
	called bool
	got    task.Task
}

func (s *mockCaptureUpdateStore) Update(ctx context.Context, t task.Task) error {
	s.called = true
	s.got = t

	return s.Store.Update(ctx, t)
}

func TestUpdateTask(t *testing.T) {
	tests := []struct {
		name            string
		id              string
		body            string
		wantStatusCode  int
		wantCalled      bool
		wantTask        *oapigen.Task
		wantErrContains []string
	}{
		{
			name:            "invalid ID",
			id:              "abc",
			body:            "{}",
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"invalid"},
		},
		{
			name:            "empty body",
			id:              validID.String(),
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"required"},
		},
		{
			name:            "malformed body",
			id:              validID.String(),
			body:            `{"title":"abc"`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"malformed"},
		},
		{
			name:            "wrong field type",
			id:              validID.String(),
			body:            `{"title":123}`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"wrong type"},
		},
		{
			name:            "non-object body",
			id:              validID.String(),
			body:            `[]`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"JSON object"},
		},
		{
			name:            "invalid due_at",
			id:              validID.String(),
			body:            `{"due_at":"tomorrow"}`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"RFC3339"},
		},
		{
			name:           "partial update",
			id:             validID.String(),
			body:           `{"title":"new"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Title = "new" })),
		},
		{
			name:           "set description to blank",
			id:             validID.String(),
			body:           `{"description":""}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Description = "" })),
		},
		{
			name:           "valid due_at in UTC",
			id:             validID.String(),
			body:           `{"due_at":"2026-12-31T10:00:00Z"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask: new(apiTaskWith(func(t *task.Task) {
				t.DueAt = new(time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC))
			})),
		},
		{
			// 10:00 at +07:00 is 03:00 UTC
			name:           "valid due_at with timezone offset",
			id:             validID.String(),
			body:           `{"due_at":"2026-12-31T10:00:00+07:00"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask: new(apiTaskWith(func(t *task.Task) {
				t.DueAt = new(time.Date(2026, 12, 31, 3, 0, 0, 0, time.UTC))
			})),
		},
		{
			name:           "valid status todo",
			id:             validID.String(),
			body:           `{"status":"todo"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Status = task.StatusTodo })),
		},
		{
			name:           "valid status doing",
			id:             validID.String(),
			body:           `{"status":"doing"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Status = task.StatusDoing })),
		},
		{
			name:           "valid status done",
			id:             validID.String(),
			body:           `{"status":"done"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Status = task.StatusDone })),
		},
		{
			name:           "empty object leaves unchanged",
			id:             validID.String(),
			body:           `{}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantTask:       new(oapigen.ToAPITask(ownerAccess(defaultTask()))),
		},
		{
			name:            "valid id but not found",
			id:              notFoundID.String(),
			body:            `{}`,
			wantStatusCode:  http.StatusNotFound,
			wantCalled:      false,
			wantErrContains: []string{"not found"},
		},
		{
			name:            "invalid title",
			id:              validID.String(),
			body:            `{"title":""}`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"title", "at least 1", "at most 200", "characters"},
		},
		{
			name:            "invalid status",
			id:              validID.String(),
			body:            `{"status":"bogus"}`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"status", "invalid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &mockCaptureUpdateStore{Store: memory.NewTaskStore()}
			_, err := s.Create(t.Context(), defaultTask())
			if err != nil {
				t.Fatalf("can not seed task for update test: %v", err)
			}
			h, _ := setupTestWithStore(t, s)
			rec, req := prepareTestRequest(t, http.MethodPatch, "/tasks/"+tt.id, tt.body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if s.called != tt.wantCalled {
				t.Fatalf("called = %t, want %t", s.called, tt.wantCalled)
			}

			if tt.wantTask != nil {
				gotTask := decodeResponseBody[oapigen.Task](t, res.Body)
				// We don't focus on the UpdatedAt field here
				// since it's in a separate suite for Patch() method of Task
				checkAPITask(t, gotTask, *tt.wantTask, cmpopts.IgnoreFields(oapigen.Task{}, "UpdatedAt"))
			}

			if tt.wantStatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

type mockCaptureDeleteStore struct {
	task.Store
	called bool
	gotID  uuid.UUID
}

func (s *mockCaptureDeleteStore) Delete(ctx context.Context, id uuid.UUID) error {
	s.called = true
	s.gotID = id

	return s.Store.Delete(ctx, id)
}

func TestDeleteTask(t *testing.T) {
	tests := []struct {
		name            string
		id              string
		wantStatusCode  int
		wantCalled      bool
		wantID          uuid.UUID
		wantErrContains []string
	}{
		{
			name:            "invalid ID",
			id:              "abc",
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      false,
			wantErrContains: []string{"invalid"},
		},
		{
			name:            "valid ID but not found",
			id:              notFoundID.String(),
			wantStatusCode:  http.StatusNotFound,
			wantCalled:      false,
			wantErrContains: []string{"not found"},
		},
		{
			name:           "success",
			id:             validID.String(),
			wantStatusCode: http.StatusNoContent,
			wantCalled:     true,
			wantID:         validID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &mockCaptureDeleteStore{Store: memory.NewTaskStore()}
			_, err := s.Create(t.Context(), defaultTask())
			if err != nil {
				t.Fatalf("can not seed task for delete test: %v", err)
			}
			h, _ := setupTestWithStore(t, s)
			rec, req := prepareTestRequest(t, http.MethodDelete, "/tasks/"+tt.id, nil)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if s.called != tt.wantCalled {
				t.Fatalf("called = %t, want %t", s.called, tt.wantCalled)
			}

			if s.gotID != tt.wantID {
				t.Fatalf("ID = %v, want %v", s.gotID, tt.wantID)
			}

			if res.StatusCode == http.StatusNoContent {
				_, err := s.Get(t.Context(), testUserID, uuid.MustParse(tt.id))
				if err == nil {
					t.Fatal("task is still found, want not found after deletion")
				}
				// Other errors won't be handled here
				// since they are tested in the TestGetOne()
			}

			if res.StatusCode >= 400 {
				checkErrorMesssage(t, getErrorMessageFromResponseBody(t, res.Body), tt.wantErrContains)
			}
		})
	}
}

// mockRoleStore shows every task to the caller with role, and records writes.
type mockRoleStore struct {
	task.Store
	role    task.Role
	updated bool
	deleted bool
}

func (s *mockRoleStore) Get(context.Context, uuid.UUID, uuid.UUID) (task.Access, error) {
	return task.Access{Task: defaultTask(), Role: s.role}, nil
}

func (s *mockRoleStore) GetForUpdate(context.Context, uuid.UUID) (task.Task, error) {
	return defaultTask(), nil
}

func (s *mockRoleStore) Update(context.Context, task.Task) error {
	s.updated = true
	return nil
}

func (s *mockRoleStore) Delete(context.Context, uuid.UUID) error {
	s.deleted = true
	return nil
}

func TestTaskRolePermissions(t *testing.T) {
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
			s := &mockRoleStore{role: tt.role}
			h, _ := setupTestWithStore(t, s)
			rec, req := prepareTestRequest(t, tt.method, "/tasks/"+validID.String(), tt.body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %d, want %d", res.StatusCode, tt.wantStatusCode)
			}

			if written := s.updated || s.deleted; written != tt.wantWritten {
				t.Errorf("store written = %t, want %t", written, tt.wantWritten)
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
	other := taskWith(func(tk *task.Task) { tk.OwnerID = uuid.MustParse("01a142fd-186e-7169-95c2-a186f9103a99") })

	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			store := memory.NewTaskStore()
			if _, err := store.Create(t.Context(), other); err != nil {
				t.Fatalf("seed task: %v", err)
			}

			var body any
			if method == http.MethodPatch {
				body = `{"title":"hijacked"}`
			}

			h, _ := setupTestWithStore(t, store)
			rec, req := prepareTestRequest(t, method, "/tasks/"+other.ID.String(), body)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != http.StatusNotFound {
				t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusNotFound)
			}

			got, err := store.GetForUpdate(t.Context(), other.ID)
			if err != nil {
				t.Fatalf("task is gone after %s: %v", method, err)
			}
			if got.Title != other.Title {
				t.Errorf("title = %q, want %q (unchanged)", got.Title, other.Title)
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
	storeEndpoints := []struct {
		name, method, path string
		body               any
	}{
		{"create", http.MethodPost, "/tasks", map[string]any{"title": "x"}},
		{"list", http.MethodGet, "/tasks", nil},
		{"get one", http.MethodGet, "/tasks/" + validID.String(), nil},
		{"update", http.MethodPatch, "/tasks/" + validID.String(), map[string]any{"title": "y"}},
		{"delete", http.MethodDelete, "/tasks/" + validID.String(), nil},
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
				h, logRec := setupTestWithStore(t, mockFailingStore{err: ec.err})
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
