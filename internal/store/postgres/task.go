// Package postgres implements [task.Store] on top of Postgres.
package postgres

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
)

// TaskStore implements [task.Store] on top of Postgres.
type TaskStore struct {
	q *sqlcgen.Queries
}

var _ task.Store = (*TaskStore)(nil)

func NewTaskStore(q *sqlcgen.Queries) *TaskStore {
	return &TaskStore{q: q}
}

// Create implements [task.Store].
func (s *TaskStore) Create(ctx context.Context, t task.Task) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("create", "task", t.ID.String(), err)
	}

	if err := s.q.CreateTask(ctx, sqlcgen.CreateTaskParams(t)); err != nil {
		return task.Task{}, formatError(err)
	}

	return t, nil
}

// Delete implements [task.Store].
func (s *TaskStore) Delete(ctx context.Context, id uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("delete", "task", id.String(), err)
	}

	affected, err := s.q.DeleteTask(ctx, id)
	if err != nil {
		return formatError(err)
	}

	if affected == 0 {
		return task.ErrNotFound
	}

	return nil
}

// Get implements [task.Store].
func (s *TaskStore) Get(ctx context.Context, viewer, id uuid.UUID) (task.Access, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "task", id.String(), err)
	}

	row, err := s.q.GetTask(ctx, sqlcgen.GetTaskParams{ID: id, Viewer: viewer})
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Access{}, task.ErrNotFound
	}

	if err != nil {
		return task.Access{}, formatError(err)
	}

	return task.Access{
		Task:          taskFromRow(row.Task),
		Role:          task.Role(row.Role),
		AssigneeEmail: row.AssigneeEmail,
	}, nil
}

// List implements [task.Store].
func (s *TaskStore) List(ctx context.Context, viewer uuid.UUID, f task.ListFilter) ([]task.Access, error) {
	formatError := func(err error) error {
		return formatPostgresError("list", "task", "", err)
	}

	rows, err := s.q.ListTasks(ctx, sqlcgen.ListTasksParams{
		Limit:        int32(f.Limit),
		Offset:       int32(f.Offset),
		Viewer:       viewer,
		Status:       (*string)(f.Status),
		AssignedToMe: f.AssignedToMe,
		Scope:        string(f.Scope),
	})

	if err != nil {
		return nil, formatError(err)
	}

	accesses := make([]task.Access, len(rows))
	for i, r := range rows {
		accesses[i] = task.Access{
			Task:          taskFromRow(r.Task),
			Role:          task.Role(r.Role),
			AssigneeEmail: r.AssigneeEmail,
		}
	}
	return accesses, nil
}

// GetForUpdate implements [task.Store].
func (s *TaskStore) GetForUpdate(ctx context.Context, id uuid.UUID) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("get for update", "task", id.String(), err)
	}

	row, err := s.q.GetTaskForUpdate(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return task.Task{}, task.ErrNotFound
	case err != nil:
		return task.Task{}, formatError(err)
	default:
		return taskFromRow(row), nil
	}
}

// Update implements [task.Store].
func (s *TaskStore) Update(ctx context.Context, t task.Task) error {
	formatError := func(err error) error {
		return formatPostgresError("update", "task", t.ID.String(), err)
	}

	affected, err := s.q.UpdateTask(ctx, sqlcgen.UpdateTaskParams{
		Title: t.Title, Description: t.Description, Status: t.Status,
		DueAt: t.DueAt, UpdatedAt: t.UpdatedAt, ID: t.ID,
	})
	switch {
	case err != nil:
		return formatError(err)
	case affected == 0:
		return task.ErrNotFound
	default:
		return nil
	}
}

// MarkOverdue implements [task.Store].
func (s *TaskStore) MarkOverdue(ctx context.Context, now time.Time) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("mark overdue", "task", "", err)
	}

	affected, err := s.q.MarkOverdue(ctx, now)
	if err != nil {
		return 0, formatError(err)
	}

	return affected, nil
}

// ClearAssignee implements [task.Store].
func (s *TaskStore) ClearAssignee(ctx context.Context, id uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("clear", "task assignee", "", err)
	}

	affected, err := s.q.ClearTaskAssignee(ctx, id)
	switch {
	case err != nil:
		return formatError(err)
	case affected == 0:
		return task.ErrNotFound
	default:
		return nil
	}
}

// ClearAssigneeIf implements [task.Store].
func (s *TaskStore) ClearAssigneeIf(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("clear", "task assignee", "", err)
	}

	if err := s.q.ClearTaskAssigneeIf(ctx, sqlcgen.ClearTaskAssigneeIfParams{
		ID: id, AssigneeID: &userID,
	}); err != nil {
		return formatError(err)
	}

	return nil
}

// SetAssignee implements [task.Store].
func (s *TaskStore) SetAssignee(ctx context.Context, id uuid.UUID, assigneeID uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("set", "task assignee", "", err)
	}

	affected, err := s.q.SetTaskAssignee(ctx, sqlcgen.SetTaskAssigneeParams{
		ID: id, AssigneeID: &assigneeID,
	})
	switch {
	case err != nil:
		return formatError(err)
	case affected == 0:
		return task.ErrNotFound
	default:
		return nil
	}
}

func taskFromRow(r sqlcgen.Task) task.Task {
	t := task.Task(r)
	t.CreatedAt = utc(t.CreatedAt)
	t.UpdatedAt = utc(t.UpdatedAt)
	t.DueAt = utcPtr(t.DueAt)

	return t
}

func NewTaskStores(q *sqlcgen.Queries) task.Stores {
	return task.Stores{
		Tasks:       NewTaskStore(q),
		Invitations: NewInvitationStore(q),
		Members:     NewMemberStore(q),
		Outbox:      NewOutboxStore(q),
		Users:       NewUserStore(q),
	}
}
