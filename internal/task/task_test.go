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
		start    *task.Task // task to patch, defaults to defaultTask()
		now      time.Time  // time passed to Patch(), defaults to testUpdatedAtAfter
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

		{
			name: "title with exactly 200 chars",
			patch: task.Patch{
				Title: new(strings.Repeat("a", 200)),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Title = strings.Repeat("a", 200)
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "title with exactly 200 multi-byte chars",
			patch: task.Patch{
				Title: new(strings.Repeat("é", 200)),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Title = strings.Repeat("é", 200)
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "title with 200 chars plus surrounding spaces is trimmed before counting",
			patch: task.Patch{
				Title: new("  " + strings.Repeat("a", 200) + "  "),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Title = strings.Repeat("a", 200)
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "title with only whitespace",
			patch: task.Patch{
				Title: new(" \t\n  "),
			},
			wantErr: task.ErrInvalidInput,
		},
		{
			name: "title is trimmed",
			patch: task.Patch{
				Title: new("  Updated task title \n"),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Title = "Updated task title"
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "description with exactly 2000 chars",
			patch: task.Patch{
				Description: new(strings.Repeat("a", 2000)),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Description = strings.Repeat("a", 2000)
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "description with exactly 2000 multi-byte chars",
			patch: task.Patch{
				Description: new(strings.Repeat("é", 2000)),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Description = strings.Repeat("é", 2000)
				t.UpdatedAt = testUpdatedAtAfter.UTC().Truncate(time.Microsecond)
			}),
		},

		{
			name: "non-empty patch identical to the task changes nothing",
			start: new(taskWith(func(t *task.Task) {
				t.DueAt = new(testDueAt.UTC().Truncate(time.Microsecond))
			})),
			patch: task.Patch{
				Title:       new("task.Task title"),
				Description: new("task.Task description"),
				Status:      new(task.StatusTodo),
				DueAt:       &testDueAt,
			},
			wantTask: taskWith(func(t *task.Task) {
				t.DueAt = new(testDueAt.UTC().Truncate(time.Microsecond))
				t.UpdatedAt = testUpdatedAtBefore.UTC().Truncate(time.Microsecond)
			}),
		},
		{
			name: "title differing only by surrounding whitespace changes nothing",
			patch: task.Patch{
				Title: new("  task.Task title  "),
			},
			wantTask: defaultTask(),
		},
		{
			name: "due_at in another timezone for the same instant changes nothing",
			start: new(taskWith(func(t *task.Task) {
				t.DueAt = new(testDueAt.UTC().Truncate(time.Microsecond))
			})),
			patch: task.Patch{
				DueAt: new(testDueAt.In(time.FixedZone("+07:00", 7*60*60))),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.DueAt = new(testDueAt.UTC().Truncate(time.Microsecond))
			}),
		},
		{
			name: "due_at differing only below a microsecond changes nothing",
			start: new(taskWith(func(t *task.Task) {
				t.DueAt = new(testDueAt.UTC().Truncate(time.Microsecond))
			})),
			patch: task.Patch{
				DueAt: new(testDueAt.Add(500 * time.Nanosecond)),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.DueAt = new(testDueAt.UTC().Truncate(time.Microsecond))
			}),
		},

		{
			// 2026-10-01T00:00:30.123456789+07:00 is 2026-09-30T17:00:30.123456Z
			name: "now with timezone and nanoseconds is normalized to UTC microseconds",
			now:  time.Date(2026, 10, 1, 0, 0, 30, 123456789, time.FixedZone("+07:00", 7*60*60)),
			patch: task.Patch{
				Title: new("Updated task title"),
			},
			wantTask: taskWith(func(t *task.Task) {
				t.Title = "Updated task title"
				t.UpdatedAt = time.Date(2026, 9, 30, 17, 0, 30, 123456000, time.UTC)
			}),
		},
		{
			name: "now with timezone and nanoseconds is ignored when nothing changes",
			now:  time.Date(2026, 10, 1, 0, 0, 30, 123456789, time.FixedZone("+07:00", 7*60*60)),
			patch: task.Patch{
				Title: new("task.Task title"),
			},
			wantTask: defaultTask(),
		},

		{
			name: "valid title then invalid description is not applied",
			patch: task.Patch{
				Title:       new("Updated task title"),
				Description: new(strings.Repeat("a", 2001)),
			},
			wantErr: task.ErrInvalidInput,
		},
		{
			name: "valid fields then invalid status is not applied",
			patch: task.Patch{
				Title:       new("Updated task title"),
				Description: new("Updated task description"),
				Status:      new(task.StatusOverdue),
				DueAt:       &testDueAt,
			},
			wantErr: task.ErrInvalidPatchStatus,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := defaultTask()
			if tt.start != nil {
				task = *tt.start
			}
			now := testUpdatedAtAfter
			if !tt.now.IsZero() {
				now = tt.now
			}
			before := task
			gotErr := task.Patch(tt.patch, now)

			if tt.wantErr != nil {
				if gotErr == nil {
					t.Fatalf("task.Patch() error = nil, want error matching %v", tt.wantErr)
				}

				if !errors.Is(gotErr, tt.wantErr) {
					t.Fatalf("task.Patch() error = %v, want error matching %v", gotErr, tt.wantErr)
				}

				// A failed patch must leave the task untouched.
				tasktest.CheckTask(t, task, before)

				return
			}

			if gotErr != nil {
				t.Fatalf("task.Patch() unexpected error: %v", gotErr)
			}

			tasktest.CheckTask(t, task, tt.wantTask)
		})
	}
}

// newTaskWith returns the task New() creates from just a title at testCreatedAt, modified by mod.
func newTaskWith(mod func(*task.Task)) task.Task {
	t := taskWith(func(t *task.Task) {
		t.Title = "Task title"
		t.Description = ""
		t.Status = task.StatusTodo
		t.DueAt = nil
		t.CreatedAt = testCreatedAt.UTC().Truncate(time.Microsecond)
		t.UpdatedAt = testCreatedAt.UTC().Truncate(time.Microsecond)
	})
	mod(&t)
	return t
}

func TestNew(t *testing.T) {
	tests := []struct {
		name     string
		now      time.Time // time passed to New(), defaults to testCreatedAt
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
		{
			name:   "title with exactly 200 chars",
			params: task.NewParams{Title: strings.Repeat("a", 200)},
			wantTask: newTaskWith(func(t *task.Task) {
				t.Title = strings.Repeat("a", 200)
			}),
		},
		{
			name:    "title with 201 chars",
			params:  task.NewParams{Title: strings.Repeat("a", 201)},
			wantErr: task.ErrInvalidInput,
		},
		{
			name:    "title with only whitespace",
			params:  task.NewParams{Title: " \t\n  "},
			wantErr: task.ErrInvalidInput,
		},
		{
			name:   "title is trimmed",
			params: task.NewParams{Title: "  Task title \n"},
			wantTask: newTaskWith(func(t *task.Task) {
				t.Title = "Task title"
			}),
		},
		{
			name:   "description with exactly 2000 chars",
			params: task.NewParams{Title: "Task title", Description: new(strings.Repeat("a", 2000))},
			wantTask: newTaskWith(func(t *task.Task) {
				t.Description = strings.Repeat("a", 2000)
			}),
		},
		{
			name:    "description with 2001 chars",
			params:  task.NewParams{Title: "Task title", Description: new(strings.Repeat("a", 2001))},
			wantErr: task.ErrInvalidInput,
		},
		{
			// 2026-10-01T00:00:30.123456789+07:00 is 2026-09-30T17:00:30.123456Z
			name:   "now with timezone and nanoseconds is normalized to UTC microseconds",
			now:    time.Date(2026, 10, 1, 0, 0, 30, 123456789, time.FixedZone("+07:00", 7*60*60)),
			params: task.NewParams{Title: "Task title"},
			wantTask: newTaskWith(func(t *task.Task) {
				at := time.Date(2026, 9, 30, 17, 0, 30, 123456000, time.UTC)
				t.CreatedAt = at
				t.UpdatedAt = at
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := testCreatedAt
			if !tt.now.IsZero() {
				now = tt.now
			}
			gotTask, gotErr := task.New(tt.params, now)

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
