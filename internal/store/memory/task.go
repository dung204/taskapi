// Package memory implements [task.Store] in process memory.
package memory

import (
	"context"
	"slices"
	"sync"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

// TaskStore implements [task.Store] in process memory. Its data is neither
// shared between instances nor kept across restarts.
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

// Get implements [task.Store]. Only the owner can see a task: the store knows
// nothing about members, so the viewer's role is always [task.RoleOwner] and
// the assignee's email is always nil.
func (s *TaskStore) Get(ctx context.Context, viewer, id uuid.UUID) (task.Access, error) {
	if ctx.Err() != nil {
		return task.Access{}, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	t, found := s.tasks[id]
	if !found || t.OwnerID != viewer {
		return task.Access{}, task.ErrNotFound
	}

	return task.Access{Task: t, Role: task.RoleOwner}, nil
}

// List implements [task.Store]. Only tasks owned by viewer are returned, so
// [task.ScopeShared] matches nothing.
func (s *TaskStore) List(ctx context.Context, viewer uuid.UUID, f task.ListFilter) ([]task.Access, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]task.Access, 0)

	if f.Scope != task.ScopeShared {
		for _, t := range s.tasks {
			if t.OwnerID != viewer || (f.Status != nil && t.Status != *f.Status) {
				continue
			}
			if f.AssignedToMe && (t.AssigneeID == nil || *t.AssigneeID != viewer) {
				continue
			}

			result = append(result, task.Access{Task: t, Role: task.RoleOwner})
		}
	}

	slices.SortFunc(result, func(a, b task.Access) int {
		return b.Task.ID.Compare(a.Task.ID)
	})

	start := min(f.Offset, len(result))
	end := min(start+f.Limit, len(result))

	return result[start:end], nil
}

// GetForUpdate implements [task.Store]. There are no transactions, so it takes
// no lock beyond the read.
func (s *TaskStore) GetForUpdate(ctx context.Context, id uuid.UUID) (task.Task, error) {
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

// Update implements [task.Store].
func (s *TaskStore) Update(ctx context.Context, t task.Task) error {
	return s.modify(ctx, t.ID, func(cur *task.Task) {
		cur.Title = t.Title
		cur.Description = t.Description
		cur.Status = t.Status
		cur.DueAt = t.DueAt
		cur.UpdatedAt = t.UpdatedAt
	})
}

// SetAssignee implements [task.Store].
func (s *TaskStore) SetAssignee(ctx context.Context, id, assigneeID uuid.UUID) error {
	return s.modify(ctx, id, func(cur *task.Task) { cur.AssigneeID = &assigneeID })
}

// ClearAssignee implements [task.Store].
func (s *TaskStore) ClearAssignee(ctx context.Context, id uuid.UUID) error {
	return s.modify(ctx, id, func(cur *task.Task) { cur.AssigneeID = nil })
}

// ClearAssigneeIf implements [task.Store].
func (s *TaskStore) ClearAssigneeIf(ctx context.Context, id, userID uuid.UUID) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if t, found := s.tasks[id]; found && t.AssigneeID != nil && *t.AssigneeID == userID {
		t.AssigneeID = nil
		s.tasks[id] = t
	}

	return nil
}

// modify applies fn to the task with the given id under the write lock.
func (s *TaskStore) modify(ctx context.Context, id uuid.UUID, fn func(t *task.Task)) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, found := s.tasks[id]
	if !found {
		return task.ErrNotFound
	}

	fn(&t)
	s.tasks[id] = t
	return nil
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
