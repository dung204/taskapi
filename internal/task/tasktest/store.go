package tasktest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dung204/taskapi/internal/task"
)

func TestStore(t *testing.T, newStore func(t *testing.T) task.Store) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	// seed creates a task through the store with the given status and due date.
	seed := func(t *testing.T, s task.Store, status task.Status, dueAt *time.Time) task.Task {
		t.Helper()

		nt, err := task.New(task.NewParams{Title: "seed", Status: status, DueAt: dueAt}, base)
		if err != nil {
			t.Fatalf("task.New: %v", err)
		}

		created, err := s.Create(context.Background(), *nt)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}

		return created
	}

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

		got, err := s.Get(context.Background(), done.ID)
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

		got, err := s.Get(context.Background(), created.ID)
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

		got, err := s.Get(context.Background(), created.ID)
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

		got, err := s.Get(context.Background(), created.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		want := created
		want.Title = title
		want.Description = desc
		want.Status = status
		want.DueAt = &due

		// updated_at comes from the store's clock, so skip it by equalizing.
		want.UpdatedAt = got.UpdatedAt

		CheckTask(t, got, want)
	})
}
