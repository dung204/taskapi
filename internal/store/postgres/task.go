// Package postgres implements [task.Store] on top of Postgres.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
)

type TaskStore struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

var _ task.Store = (*TaskStore)(nil)

func NewTaskStore(pool *pgxpool.Pool) *TaskStore {
	return &TaskStore{
		pool: pool,
		q:    sqlcgen.New(pool),
	}
}

// Create implements [task.Store].
func (s *TaskStore) Create(ctx context.Context, t task.Task) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("create", t.ID.String(), err)
	}

	err := s.q.CreateTask(ctx, sqlcgen.CreateTaskParams{
		ID: t.ID, Title: t.Title, Description: t.Description, Status: t.Status,
		DueAt: t.DueAt, CreatedAt: t.CreatedAt, UpdatedAt: t.CreatedAt,
	})

	if err != nil {
		return task.Task{}, formatError(err)
	}

	return t, nil
}

// Delete implements [task.Store].
func (s *TaskStore) Delete(ctx context.Context, id uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("delete", id.String(), err)
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
func (s *TaskStore) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", id.String(), err)
	}

	row, err := s.q.GetTask(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, task.ErrNotFound
	}

	if err != nil {
		return task.Task{}, formatError(err)
	}

	return taskFromRow(row), nil
}

// List implements [task.Store].
func (s *TaskStore) List(ctx context.Context, f task.ListFilter) ([]task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("list", "", err)
	}

	var rows []sqlcgen.Task
	var err error

	if f.Status == "" {
		rows, err = s.q.ListTasks(ctx, sqlcgen.ListTasksParams{
			Limit: int32(f.Limit), Offset: int32(f.Offset),
		})
	} else {
		rows, err = s.q.ListTasksByStatus(ctx, sqlcgen.ListTasksByStatusParams{
			Status: f.Status, Limit: int32(f.Limit), Offset: int32(f.Offset),
		})
	}
	if err != nil {
		return nil, formatError(err)
	}

	tasks := make([]task.Task, len(rows))
	for i, r := range rows {
		tasks[i] = taskFromRow(r)
	}
	return tasks, nil
}

// Update implements [task.Store].
func (s *TaskStore) Update(ctx context.Context, id uuid.UUID, p task.Patch) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("update", id.String(), err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return task.Task{}, formatError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)

	row, err := q.GetTaskForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return task.Task{}, task.ErrNotFound
	}

	if err != nil {
		return task.Task{}, formatError(err)
	}

	t := taskFromRow(row)
	if err = t.Patch(p, time.Now()); err != nil {
		return task.Task{}, err
	}

	affected, err := q.UpdateTask(ctx, sqlcgen.UpdateTaskParams{
		Title: t.Title, Description: t.Description, Status: t.Status,
		DueAt: t.DueAt, UpdatedAt: t.UpdatedAt, ID: t.ID,
	})
	if err != nil {
		return task.Task{}, formatError(err)
	}

	if affected == 0 {
		return task.Task{}, task.ErrNotFound
	}

	if err = tx.Commit(ctx); err != nil {
		return task.Task{}, formatError(err)
	}

	return t, nil
}

// MarkOverdue implements [task.Store].
func (s *TaskStore) MarkOverdue(ctx context.Context, now time.Time) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("mark overdue", "", err)
	}

	affected, err := s.q.MarkOverdue(ctx, now)
	if err != nil {
		return 0, formatError(err)
	}

	return affected, nil
}

func formatPostgresError(op string, taskID string, err error) error {
	id := ""
	if taskID != "" {
		id = " " + taskID
	}

	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return fmt.Errorf("postgres: %s task%s: %w: %w", op, id, task.ErrServiceUnavailable, err)
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case "57P01", "57P02", "57P03":
			return fmt.Errorf("postgres: %s task%s: %w: %w", op, id, task.ErrServiceUnavailable, err)
		}
	}

	return fmt.Errorf("postgres: %s task%s: %w", op, id, err)
}

func taskFromRow(r sqlcgen.Task) task.Task {
	t := task.Task(r)
	t.CreatedAt = t.CreatedAt.UTC()
	t.UpdatedAt = t.UpdatedAt.UTC()
	if t.DueAt != nil {
		t.DueAt = new(t.DueAt.UTC())
	}

	return t
}
