package memory

import (
	"context"
	"slices"
	"sync"
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

	if _, found := store.tasks[id]; !found {
		return task.ErrNotFound
	}

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
		return task.Task{}, task.ErrNotFound
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
		if f.Status != "" && t.Status != f.Status {
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

	t, found := store.tasks[id]

	if !found {
		return task.Task{}, task.ErrNotFound
	}

	t.Patch(p)

	store.tasks[id] = t
	return t, nil
}
