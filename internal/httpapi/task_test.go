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

	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/logtest"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

var validID = uuid.MustParse("01a142e5-6a3e-7218-9722-c0215445fae0")
var notFoundID = uuid.MustParse("01a142fd-186e-7169-95c2-a186f9103a97")

func defaultTask() task.Task {
	return task.Task{
		ID:          validID,
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

func apiTaskWith(mod func(*task.Task)) oapigen.Task {
	return oapigen.ToAPITask(taskWith(mod))
}

// checkAPITask adapts oapigen.Task to the task.Task that tasktest.CheckTask expects.
func checkAPITask(t *testing.T, got, want oapigen.Task) {
	t.Helper()
	toTask := func(a oapigen.Task) task.Task {
		return task.Task{
			ID:          a.Id,
			Title:       a.Title,
			Description: a.Description,
			Status:      a.Status,
			DueAt:       a.DueAt,
			CreatedAt:   a.CreatedAt,
			UpdatedAt:   a.UpdatedAt,
		}
	}
	tasktest.CheckTask(t, toTask(got), toTask(want))
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

				tt.wantTask.Id = got.Id
				tt.wantTask.CreatedAt = got.CreatedAt
				tt.wantTask.UpdatedAt = got.UpdatedAt
				checkAPITask(t, got, *tt.wantTask)
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
	tasks []task.Task
}

func (s *mockCaptureListStore) List(_ context.Context, f task.ListFilter) ([]task.Task, error) {
	s.got = f
	return s.tasks, nil
}

func defaultListFilter() task.ListFilter {
	return task.ListFilter{
		Limit:  20,
		Offset: 0,
		Status: task.Status(""),
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
	if got.Status != want.Status {
		t.Errorf("status = %q, want %q", got.Status, want.Status)
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
				Status: task.StatusDone,
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
			wantFilter:     listFilterWith(func(lf *task.ListFilter) { lf.Status = task.StatusOverdue }),
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
			ID:     testID(i),
			Title:  fmt.Sprintf("task %d", i),
			Status: status,
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

func (s mockGetOneStore) Get(_ context.Context, id uuid.UUID) (task.Task, error) {
	if id == s.found.ID {
		return s.found, nil
	}

	return task.Task{}, task.ErrNotFound
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
			wantTask:       new(oapigen.ToAPITask(defaultTask())),
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
	called   bool
	gotID    uuid.UUID
	gotPatch task.Patch
}

func (s *mockCaptureUpdateStore) Update(ctx context.Context, id uuid.UUID, p task.Patch) (task.Task, error) {
	s.called = true
	s.gotID = id
	s.gotPatch = p

	return s.Store.Update(ctx, id, p)
}

func TestUpdateTask(t *testing.T) {
	tests := []struct {
		name            string
		id              string
		body            string
		wantStatusCode  int
		wantCalled      bool
		wantID          uuid.UUID
		wantPatch       task.Patch
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
			wantID:         validID,
			wantPatch:      task.Patch{Title: new("new")},
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Title = "new" })),
		},
		{
			name:           "set description to blank",
			id:             validID.String(),
			body:           `{"description":""}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantID:         validID,
			wantPatch:      task.Patch{Description: new("")},
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Description = "" })),
		},
		{
			name:           "valid due_at in UTC",
			id:             validID.String(),
			body:           `{"due_at":"2026-12-31T10:00:00Z"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantID:         validID,
			wantPatch:      task.Patch{DueAt: new(time.Date(2026, 12, 31, 10, 0, 0, 0, time.UTC))},
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
			wantID:         validID,
			wantPatch:      task.Patch{DueAt: new(time.Date(2026, 12, 31, 3, 0, 0, 0, time.UTC))},
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
			wantID:         validID,
			wantPatch:      task.Patch{Status: new(task.StatusTodo)},
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Status = task.StatusTodo })),
		},
		{
			name:           "valid status doing",
			id:             validID.String(),
			body:           `{"status":"doing"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantID:         validID,
			wantPatch:      task.Patch{Status: new(task.StatusDoing)},
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Status = task.StatusDoing })),
		},
		{
			name:           "valid status done",
			id:             validID.String(),
			body:           `{"status":"done"}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantID:         validID,
			wantPatch:      task.Patch{Status: new(task.StatusDone)},
			wantTask:       new(apiTaskWith(func(t *task.Task) { t.Status = task.StatusDone })),
		},
		{
			name:           "empty object leaves unchanged",
			id:             validID.String(),
			body:           `{}`,
			wantStatusCode: http.StatusOK,
			wantCalled:     true,
			wantID:         validID,
			wantPatch:      task.Patch{},
			wantTask:       new(oapigen.ToAPITask(defaultTask())),
		},
		{
			name:            "valid id but not found",
			id:              notFoundID.String(),
			body:            `{}`,
			wantStatusCode:  http.StatusNotFound,
			wantCalled:      true,
			wantID:          notFoundID,
			wantErrContains: []string{"not found"},
		},
		{
			name:            "invalid title",
			id:              validID.String(),
			body:            `{"title":""}`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      true,
			wantID:          validID,
			wantPatch:       task.Patch{Title: new("")},
			wantErrContains: []string{"title", "at least 1", "at most 200", "characters"},
		},
		{
			name:            "invalid status",
			id:              validID.String(),
			body:            `{"status":"bogus"}`,
			wantStatusCode:  http.StatusBadRequest,
			wantCalled:      true,
			wantID:          validID,
			wantPatch:       task.Patch{Status: new(task.Status("bogus"))},
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

			if s.gotID != tt.wantID {
				t.Fatalf("ID = %v, want %v", s.gotID, tt.wantID)
			}

			if hasError := tasktest.CheckPatch(t, s.gotPatch, tt.wantPatch); hasError {
				t.FailNow()
			}

			if tt.wantTask != nil {
				gotTask := decodeResponseBody[oapigen.Task](t, res.Body)
				// We don't focus on the UpdatedAt field here
				// since it's in a separate suite for Patch() method of Task
				gotTask.UpdatedAt = tt.wantTask.UpdatedAt
				checkAPITask(t, gotTask, *tt.wantTask)
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
			wantCalled:      true,
			wantID:          notFoundID,
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
				_, err := s.Get(t.Context(), uuid.MustParse(tt.id))
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

func (s mockFailingStore) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	return task.Task{}, s.err
}

func (s mockFailingStore) List(ctx context.Context, f task.ListFilter) ([]task.Task, error) {
	return []task.Task{}, s.err
}

func (s mockFailingStore) Update(ctx context.Context, id uuid.UUID, p task.Patch) (task.Task, error) {
	return task.Task{}, s.err
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

	errUnavailable := fmt.Errorf("get task: %w", task.ErrServiceUnavailable)
	errGeneric := errors.New("oops")

	errCases := []struct {
		name            string
		err             error
		wantStatusCode  int
		wantErrContains []string
		wantHidden      []string
		wantEmptyBody   bool           // the handler writes nothing (client went away)
		wantLogs        []logtest.Want // records logged by handleStoreError, before "request completed"
	}{
		{
			name:            "generic error",
			err:             errGeneric,
			wantStatusCode:  http.StatusInternalServerError,
			wantErrContains: []string{"internal"},
			wantHidden:      []string{"oops"},
			wantLogs:        []logtest.Want{{Level: slog.LevelError, Msg: "store error", Attrs: map[string]any{"error": errGeneric}}},
		},
		{
			name:            "service unavailable",
			err:             task.ErrServiceUnavailable,
			wantStatusCode:  http.StatusServiceUnavailable,
			wantErrContains: []string{"service unavailable"},
			wantLogs:        []logtest.Want{{Level: slog.LevelError, Msg: "store error", Attrs: map[string]any{"error": task.ErrServiceUnavailable}}},
		},
		{
			name:            "wrapped service unavailable",
			err:             errUnavailable,
			wantStatusCode:  http.StatusServiceUnavailable,
			wantErrContains: []string{"service unavailable"},
			wantHidden:      []string{"get task"},
			wantLogs:        []logtest.Want{{Level: slog.LevelError, Msg: "store error", Attrs: map[string]any{"error": errUnavailable}}},
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
				wantLogs := append(slices.Clone(ec.wantLogs), logtest.Want{
					Level: completedLevel,
					Msg:   "request completed",
					Attrs: map[string]any{"status": int64(ec.wantStatusCode)},
				})
				logtest.CheckLogRecords(t, logRec.Records(), wantLogs...)
			})
		}
	}
}
