package task_test

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

var testID = uuid.NewV7()
var testDueAt, _ = time.Parse(time.RFC3339, "2026-12-31T00:00:00.000Z")
var testCreatedAt, _ = time.Parse(time.RFC3339, "2026-10-01T00:00:00.000Z")
var testUpdatedAtBefore = testCreatedAt
var testUpdatedAtAfter = testUpdatedAtBefore.Add(30 * time.Second)

func defaultTask() task.Task {
	return task.Task{
		ID:          testID,
		Title:       "task.Task title",
		Description: "task.Task description",
		Status:      task.StatusTodo,
		DueAt:       nil,
		CreatedAt:   testCreatedAt.UTC().Truncate(time.Microsecond),
		UpdatedAt:   testUpdatedAtBefore.UTC().Truncate(time.Microsecond),
	}
}

func taskWith(mod func(*task.Task)) task.Task {
	t := defaultTask()
	mod(&t)
	return t
}

func TestPatch(t *testing.T) {
	tests := []struct {
		name     string
		patch    task.Patch
		wantTask task.Task
		wantErr  error
	}{
		{
			name:  "empty patch",
			patch: task.Patch{},
			wantTask: taskWith(func(t *task.Task) {
				t.UpdatedAt = testUpdatedAtBefore.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "valid patch",
			patch: task.Patch{
				Title:       new("Updated task title"),
				Description: new("Updated task description"),
				Status:      new(task.StatusDoing),
				DueAt:       &testDueAt,
			},
			wantTask: taskWith(func(t *task.Task) {
				dueAt := testDueAt.UTC().Truncate(time.Microsecond)

				t.Title = "Updated task title"
				t.Description = "Updated task description"
				t.Status = task.StatusDoing
				t.DueAt = &dueAt
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},

		{
			name: "empty title",
			patch: task.Patch{
				Title: new(""),
			},
			wantErr: task.ErrInvalidInput,
		},
		{
			name: "title 201 chars",
			patch: task.Patch{
				Title: new(strings.Repeat("a", 201)),
			},
			wantErr: task.ErrInvalidInput,
		},

		{
			name: "empty description clears the description",
			patch: task.Patch{
				Description: new(""),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Description = ""
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "description length over 2001 chars",
			patch: task.Patch{
				Description: new(strings.Repeat("a", 2001)),
			},
			wantErr: task.ErrInvalidInput,
		},

		{
			name: "invalid status",
			patch: task.Patch{
				Status: new(task.Status("abc")),
			},
			wantErr: task.ErrInvalidPatchStatus,
		},
		{
			name: "overdue status",
			patch: task.Patch{
				Status: new(task.StatusOverdue),
			},
			wantErr: task.ErrInvalidPatchStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := defaultTask()
			gotErr := task.Patch(tt.patch, testUpdatedAtAfter)

			if tt.wantErr != nil {
				if gotErr == nil {
					t.Fatalf("task.Patch() error = nil, want error matching %v", tt.wantErr)
				}

				if !errors.Is(gotErr, tt.wantErr) {
					t.Fatalf("task.Patch() error = %v, want error matching %v", gotErr, tt.wantErr)
				}

				return
			}

			if gotErr != nil {
				t.Fatalf("task.Patch() unexpected error: %v", gotErr)
			}

			tasktest.CheckTask(t, task, tt.wantTask)
		})
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		params   task.NewParams
		wantTask task.Task
		wantErr  error
	}{
		{
			name: "defaults with valid title",
			params: task.NewParams{
				Title: "task.Task title",
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Title = "task.Task title"
				t.Description = ""
				t.Status = task.StatusTodo
				t.DueAt = nil
				t.CreatedAt = testCreatedAt.UTC().Truncate(time.Microsecond)
				t.UpdatedAt = testCreatedAt.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "defaults with invalid title",
			params: task.NewParams{
				Title: "",
			},
			wantErr: task.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTask, gotErr := task.New(tt.params, testCreatedAt)

			if tt.wantErr != nil {
				if gotErr == nil {
					t.Fatalf("task.Patch() error = nil, want error matching %v", tt.wantErr)
				}

				if !errors.Is(gotErr, tt.wantErr) {
					t.Fatalf("task.Patch() error = %v, want error matching %v", gotErr, tt.wantErr)
				}

				return
			}

			if gotErr != nil {
				t.Fatalf("task.Patch() unexpected error: %v", gotErr)
			}

			tt.wantTask.ID = gotTask.ID
			tasktest.CheckTask(t, *gotTask, tt.wantTask)
		})
	}
}
