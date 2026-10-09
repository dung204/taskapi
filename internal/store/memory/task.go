package memory

import (
	"context"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

type TaskStore struct {
	mu    sync.RWMutex
	tasks map[uuid.UUID]task.Task
}

var _ task.Store = (*TaskStore)(nil)

func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[uuid.UUID]task.Task),
	}
}

// Create implements [task.Store].
func (s *TaskStore) Create(ctx context.Context, t task.Task) (task.Task, error) {
	if ctx.Err() != nil {
		return task.Task{}, ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks[t.ID] = t
	return t, nil
}

// Delete implements [task.Store].
func (s *TaskStore) Delete(ctx context.Context, id uuid.UUID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, found := s.tasks[id]; !found {
		return task.ErrNotFound
	}

	delete(s.tasks, id)
	return nil
}

// Get implements [task.Store].
func (s *TaskStore) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	if ctx.Err() != nil {
		return task.Task{}, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	t, found := s.tasks[id]

	if !found {
		return task.Task{}, task.ErrNotFound
	}

	return t, nil
}

// List implements [task.Store].
func (s *TaskStore) List(ctx context.Context, f task.ListFilter) ([]task.Task, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]task.Task, 0)

	for _, t := range s.tasks {
		if f.Status != "" && t.Status != f.Status {
			continue
		}

		result = append(result, t)
	}

	slices.SortFunc(result, func(a, b task.Task) int {
		return b.ID.Compare(a.ID)
	})

	start := min(f.Offset, len(result))
	end := min(start+f.Limit, len(result))

	return result[start:end], nil
}

// Update implements [task.Store].
func (s *TaskStore) Update(ctx context.Context, id uuid.UUID, p task.Patch) (task.Task, error) {
	if ctx.Err() != nil {
		return task.Task{}, ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, found := s.tasks[id]

	if !found {
		return task.Task{}, task.ErrNotFound
	}

	err := t.Patch(p)

	if err != nil {
		return task.Task{}, err
	}

	s.tasks[id] = t
	return t, nil
}

// MarkOverdue implements [task.Store].
func (s *TaskStore) MarkOverdue(ctx context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int64 = 0

	for id, t := range s.tasks {
		if t.Status != task.StatusOverdue && t.Status != task.StatusDone && t.DueAt != nil && now.After(*t.DueAt) {
			t.Status = task.StatusOverdue
			t.UpdatedAt = now.UTC().Truncate(time.Microsecond)

			s.tasks[id] = t
			count++
		}
	}

	return count, nil
}
