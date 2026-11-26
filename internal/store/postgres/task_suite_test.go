package postgres

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

// checkTasks compares the tasks of got with want. The viewer in these tests
// owns every task they look at, so each access must carry the owner role.
func checkTasks(t *testing.T, got []task.Access, want []task.Task) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		tasktest.CheckTask(t, got[i].Task, want[i])
		if got[i].Role != task.RoleOwner {
			t.Errorf("role = %q, want %q", got[i].Role, task.RoleOwner)
		}
	}
}

// patchTask applies p to the task with the given id the way a caller does:
// fetch it with GetForUpdate, apply the patch, persist it with Update.
func patchTask(ctx context.Context, s task.Store, id uuid.UUID, p task.Patch, now time.Time) error {
	t, err := s.GetForUpdate(ctx, id)
	if err != nil {
		return err
	}
	if err := t.Patch(p, now); err != nil {
		return err
	}
	return s.Update(ctx, t)
}

// runTaskStoreSuite runs the behaviour suite. newStoreOwners also returns a function that
// provides a fresh owner ID on each call, because tasks belong to users and a
// real database enforces that with a foreign key.
func runTaskStoreSuite(t *testing.T, newStoreOwners func(t *testing.T) (task.Store, func() uuid.UUID)) {
	t.Helper()
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// newStore returns an empty store and sets owner to a fresh owner ID, the
	// viewer for every test that does not care about ownership. newOwner
	// provides further owners. Subtests run sequentially, so sharing these is safe.
	var (
		owner    uuid.UUID
		newOwner func() uuid.UUID
	)
	newStore := func(t *testing.T) task.Store {
		t.Helper()

		s, no := newStoreOwners(t)
		newOwner = no
		owner = no()

		return s
	}

	// seedFor creates a task owned by ownerID with the given status and due date.
	seedFor := func(t *testing.T, s task.Store, ownerID uuid.UUID, status task.Status, dueAt *time.Time) task.Task {
		t.Helper()

		nt, err := task.New(task.NewParams{Title: "seed", OwnerID: ownerID, Status: status, DueAt: dueAt}, base)
		if err != nil {
			t.Fatalf("task.New: %v", err)
		}

		created, err := s.Create(context.Background(), nt)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		return created
	}

	// seed creates a task owned by owner.
	seed := func(t *testing.T, s task.Store, status task.Status, dueAt *time.Time) task.Task {
		t.Helper()
		return seedFor(t, s, owner, status, dueAt)
	}

	// newestFirst sorts tasks by ID descending, which is the order List must return.
	newestFirst := func(tasks []task.Task) []task.Task {
		sorted := slices.Clone(tasks)
		slices.SortFunc(sorted, func(a, b task.Task) int { return b.ID.Compare(a.ID) })
		return sorted
	}

	t.Run("Get returns ErrNotFound for unknown id", func(t *testing.T) {
		s := newStore(t)

		_, err := s.Get(context.Background(), owner, uuid.NewV7())
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("Update returns ErrNotFound for unknown id", func(t *testing.T) {
		s := newStore(t)

		_, err := s.GetForUpdate(context.Background(), uuid.NewV7())
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("GetForUpdate: err = %v, want %v", err, task.ErrNotFound)
		}

		err = s.Update(context.Background(), task.Task{ID: uuid.NewV7(), Title: "new title", Status: task.StatusTodo})
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("Update: err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("Delete returns ErrNotFound for unknown id", func(t *testing.T) {
		s := newStore(t)

		err := s.Delete(context.Background(), uuid.NewV7())
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("Delete removes the task", func(t *testing.T) {
		s := newStore(t)
		created := seed(t, s, task.StatusTodo, nil)

		if err := s.Delete(context.Background(), created.ID); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		_, err := s.Get(context.Background(), owner, created.ID)
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("Get after Delete: err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("List orders by id descending", func(t *testing.T) {
		s := newStore(t)

		var all []task.Task
		for range 5 {
			all = append(all, seed(t, s, task.StatusTodo, nil))
		}

		got, err := s.List(context.Background(), owner, task.ListFilter{Limit: 10})
		if err != nil {
			t.Fatalf("List: %v", err)
		}

		checkTasks(t, got, newestFirst(all))
	})

	t.Run("List applies offset and limit", func(t *testing.T) {
		s := newStore(t)

		var all []task.Task
		for range 5 {
			all = append(all, seed(t, s, task.StatusTodo, nil))
		}
		want := newestFirst(all)

		tests := []struct {
			name   string
			filter task.ListFilter
			want   []task.Task
		}{
			{"limit only", task.ListFilter{Limit: 2}, want[:2]},
			{"offset and limit", task.ListFilter{Offset: 1, Limit: 2}, want[1:3]},
			{"limit past the end", task.ListFilter{Offset: 3, Limit: 10}, want[3:]},
			{"offset past the end", task.ListFilter{Offset: 10, Limit: 10}, nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := s.List(context.Background(), owner, tt.filter)
				if err != nil {
					t.Fatalf("List: %v", err)
				}

				checkTasks(t, got, tt.want)
			})
		}
	})

	t.Run("List filters by status", func(t *testing.T) {
		s := newStore(t)

		var doing []task.Task
		seed(t, s, task.StatusTodo, nil)
		doing = append(doing, seed(t, s, task.StatusDoing, nil))
		seed(t, s, task.StatusDone, nil)
		doing = append(doing, seed(t, s, task.StatusDoing, nil))

		got, err := s.List(context.Background(), owner, task.ListFilter{Limit: 10, Status: new(task.StatusDoing)})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		checkTasks(t, got, newestFirst(doing))

		got, err = s.List(context.Background(), owner, task.ListFilter{Limit: 10, Status: new(task.StatusOverdue)})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		checkTasks(t, got, nil)
	})

	t.Run("MarkOverdue skips done tasks", func(t *testing.T) {
		s := newStore(t)
		due := base.Add(-time.Hour)
		done := seed(t, s, task.StatusDone, &due)

		n, err := s.MarkOverdue(context.Background(), base)
		if err != nil {
			t.Fatalf("MarkOverdue: %v", err)
		}
		if n != 0 {
			t.Errorf("count = %d, want 0", n)
		}

		got, err := s.Get(context.Background(), owner, done.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		tasktest.CheckTask(t, got.Task, done)
	})

	t.Run("MarkOverdue updates updated_at", func(t *testing.T) {
		s := newStore(t)
		due := base.Add(-time.Hour)
		created := seed(t, s, task.StatusTodo, &due)

		now := base.Add(time.Minute)
		n, err := s.MarkOverdue(context.Background(), now)
		if err != nil {
			t.Fatalf("MarkOverdue: %v", err)
		}
		if n != 1 {
			t.Errorf("count = %d, want 1", n)
		}

		got, err := s.Get(context.Background(), owner, created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		want := created
		want.Status = task.StatusOverdue
		want.UpdatedAt = now.UTC().Truncate(time.Microsecond)
		tasktest.CheckTask(t, got.Task, want)

		// Already-overdue tasks are not counted or touched again.
		n, err = s.MarkOverdue(context.Background(), now.Add(time.Hour))
		if err != nil {
			t.Fatalf("second MarkOverdue: %v", err)
		}
		if n != 0 {
			t.Errorf("second count = %d, want 0", n)
		}
	})

	t.Run("MarkOverdue ignores tasks without due_at", func(t *testing.T) {
		s := newStore(t)
		created := seed(t, s, task.StatusTodo, nil)

		n, err := s.MarkOverdue(context.Background(), base.Add(24*time.Hour))
		if err != nil {
			t.Fatalf("MarkOverdue: %v", err)
		}
		if n != 0 {
			t.Errorf("count = %d, want 0", n)
		}

		got, err := s.Get(context.Background(), owner, created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		tasktest.CheckTask(t, got.Task, created)
	})

	t.Run("Update persists the patched fields", func(t *testing.T) {
		s := newStore(t)
		created := seed(t, s, task.StatusTodo, nil)

		title := "new title"
		desc := "new description"
		status := task.StatusDoing
		due := base.Add(48 * time.Hour)
		now := base.Add(time.Minute)

		// Each patch changes a different field, so the final task must
		// contain all of them.
		for _, p := range []task.Patch{{Title: &title}, {Description: &desc}, {Status: &status}, {DueAt: &due}} {
			if err := patchTask(context.Background(), s, created.ID, p, now); err != nil {
				t.Fatalf("patch: %v", err)
			}
		}

		got, err := s.Get(context.Background(), owner, created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		want := created
		want.Title = title
		want.Description = desc
		want.Status = status
		want.DueAt = &due
		want.UpdatedAt = now.UTC().Truncate(time.Microsecond)

		tasktest.CheckTask(t, got.Task, want)
	})

	t.Run("Update leaves the owner alone", func(t *testing.T) {
		s := newStore(t)
		created := seed(t, s, task.StatusTodo, nil)

		changed := created
		changed.Title = "new title"
		changed.OwnerID = newOwner()

		if err := s.Update(context.Background(), changed); err != nil {
			t.Fatalf("Update: %v", err)
		}

		got, err := s.Get(context.Background(), owner, created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		want := created
		want.Title = "new title"
		tasktest.CheckTask(t, got.Task, want)
	})

	t.Run("Get hides tasks owned by someone else", func(t *testing.T) {
		s := newStore(t)
		created := seed(t, s, task.StatusTodo, nil)

		_, err := s.Get(context.Background(), newOwner(), created.ID)
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("List only returns the viewer's tasks", func(t *testing.T) {
		s := newStore(t)
		other := newOwner()

		mine := []task.Task{
			seed(t, s, task.StatusTodo, nil),
			seed(t, s, task.StatusDoing, nil),
		}
		seedFor(t, s, other, task.StatusTodo, nil)
		seedFor(t, s, other, task.StatusDoing, nil)

		tests := []struct {
			name   string
			filter task.ListFilter
			want   []task.Task
		}{
			{"no status", task.ListFilter{Limit: 10}, newestFirst(mine)},
			{"with status", task.ListFilter{Limit: 10, Status: new(task.StatusDoing)}, mine[1:]},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := s.List(context.Background(), owner, tt.filter)
				if err != nil {
					t.Fatalf("List: %v", err)
				}

				checkTasks(t, got, tt.want)
			})
		}
	})

	t.Run("create then get again", func(t *testing.T) {
		s := newStore(t)
		created := seed(t, s, task.StatusTodo, nil)

		got, err := s.Get(context.Background(), owner, created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		tasktest.CheckTask(t, got.Task, created)
	})
}
