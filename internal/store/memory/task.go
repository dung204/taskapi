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
	tasks map[uuid.UUID]task.Task
	mu    sync.RWMutex
}

func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[uuid.UUID]task.Task),
	}
}

// Create implements [task.Store].
func (store *TaskStore) Create(ctx context.Context, t task.Task) (task.Task, error) {
	if ctx.Err() != nil {
		return task.Task{}, ctx.Err()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	store.tasks[t.ID] = t
	return t, nil
}

// Delete implements [task.Store].
func (store *TaskStore) Delete(ctx context.Context, id uuid.UUID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	delete(store.tasks, id)
	return nil
}

// Get implements [task.Store].
func (store *TaskStore) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	if ctx.Err() != nil {
		return task.Task{}, ctx.Err()
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	t, found := store.tasks[id]

	if !found {
		return t, task.ErrNotFound
	}

	return t, nil
}

// List implements [task.Store].
func (store *TaskStore) List(ctx context.Context, f task.ListFilter) ([]task.Task, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	result := make([]task.Task, 0)

	for _, t := range store.tasks {
		if t.Status == f.Status {
			continue
		}

		result = append(result, t)
	}

	slices.SortFunc(result, func(a, b task.Task) int {
		return a.ID.Compare(b.ID)
	})

	start := min(f.Offset, len(result))
	end := min(start+f.Limit, len(result))

	return result[start:end], nil
}

// Update implements [task.Store].
func (store *TaskStore) Update(ctx context.Context, id uuid.UUID, p task.Patch) (task.Task, error) {
	if ctx.Err() != nil {
		return task.Task{}, ctx.Err()
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	t, err := store.Get(ctx, id)

	if err != nil {
		return task.Task{}, err
	}

	changeUpdatedAt := false

	if p.Title != nil && *p.Title != t.Title {
		changeUpdatedAt = true
		t.Title = *p.Title
	}

	if p.Description != nil && *p.Description != t.Description {
		changeUpdatedAt = true
		t.Description = *p.Description
	}

	if p.Status != nil && *p.Status != t.Status {
		changeUpdatedAt = true
		t.Status = *p.Status
	}

	if p.DueAt != nil && p.DueAt.Compare(*t.DueAt) != 0 {
		changeUpdatedAt = true
		t.DueAt = p.DueAt
	}

	if changeUpdatedAt {
		t.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}

	store.tasks[id] = t
	return t, nil
}
