// Package tasktest holds test helpers shared by the store implementations,
// including the behaviour suite that every [task.Store] must pass.
package tasktest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dung204/taskapi/internal/task"
)

func checkTasks(t *testing.T, got, want []task.Task) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		CheckTask(t, got[i], want[i])
	}
}

// TestStore runs the behaviour suite. newStoreOwners also returns a function that
// provides a fresh owner ID on each call, because tasks belong to users and a
// real database enforces that with a foreign key.
func TestStore(t *testing.T, newStoreOwners func(t *testing.T) (task.Store, func() uuid.UUID)) {
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

		title := "new title"
		_, err := s.Update(context.Background(), uuid.NewV7(), task.Patch{Title: &title})
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
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

		got, err := s.List(context.Background(), owner, task.ListFilter{Limit: 10, Status: task.StatusDoing})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		checkTasks(t, got, newestFirst(doing))

		got, err = s.List(context.Background(), owner, task.ListFilter{Limit: 10, Status: task.StatusOverdue})
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
		CheckTask(t, got, done)
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
		CheckTask(t, got, want)

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
		CheckTask(t, got, created)
	})

	t.Run("Patch concurrent updates do not lose writes", func(t *testing.T) {
		s := newStore(t)
		created := seed(t, s, task.StatusTodo, nil)

		title := "new title"
		desc := "new description"
		status := task.StatusDoing
		due := base.Add(48 * time.Hour)

		// Each goroutine patches a different field, so if every write is
		// applied the final task must contain all of them.
		patches := []task.Patch{
			{Title: &title},
			{Description: &desc},
			{Status: &status},
			{DueAt: &due},
		}

		var wg sync.WaitGroup
		errs := make(chan error, len(patches))
		for _, p := range patches {
			wg.Go(func() {
				if _, err := s.Update(context.Background(), created.ID, p); err != nil {
					errs <- err
				}
			})
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			t.Errorf("Update: %v", err)
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

		CheckTask(t, got, want, cmpopts.IgnoreFields(task.Task{}, "UpdatedAt"))
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
			{"with status", task.ListFilter{Limit: 10, Status: task.StatusDoing}, mine[1:]},
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

		CheckTask(t, got, created)
	})
}
