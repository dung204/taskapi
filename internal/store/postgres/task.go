package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
	"github.com/jackc/pgx/v5/pgconn"
)

type scanner interface {
	Scan(dest ...any) error
}

func scanToTask(s scanner, t *task.Task) error {
	return s.Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&t.Status,
		&t.DueAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
}

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type TaskStore struct {
	db *sql.DB
}

var _ task.Store = (*TaskStore)(nil)

func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{
		db: db,
	}
}

// Create implements [task.Store].
func (s *TaskStore) Create(ctx context.Context, t task.Task) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("create", t.ID.String(), err)
	}

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO "tasks" 
			("id", "title", "description", "status", 
			"due_at", "created_at", "updated_at")
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		t.ID,
		t.Title,
		t.Description,
		t.Status,
		t.DueAt,
		t.CreatedAt,
		t.UpdatedAt,
	)

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

	res, err := s.db.ExecContext(
		ctx,
		`DELETE FROM "tasks" WHERE "id" = $1`,
		id,
	)
	if err != nil {
		return formatError(err)
	}

	affected, err := res.RowsAffected()
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

	return s.get(ctx, s.db, id, formatError)
}

type formatErrorFunc = func(err error) error

const selectTaskByID = `
SELECT 
	"id", "title", "description", "status", 
	"due_at", "created_at", "updated_at"
FROM "tasks" 
WHERE "id" = $1
`

func (s *TaskStore) get(ctx context.Context, q rowQuerier, id uuid.UUID, formatError formatErrorFunc) (task.Task, error) {
	return queryTask(ctx, q, selectTaskByID, id, formatError)
}

func (s *TaskStore) getForUpdate(ctx context.Context, tx *sql.Tx, id uuid.UUID, formatError formatErrorFunc) (task.Task, error) {
	return queryTask(ctx, tx, selectTaskByID+" FOR UPDATE", id, formatError)
}

func queryTask(ctx context.Context, q rowQuerier, query string, id uuid.UUID, formatError formatErrorFunc) (task.Task, error) {
	var t task.Task
	err := scanToTask(q.QueryRowContext(ctx, query, id), &t)

	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, task.ErrNotFound
	}

	if err != nil {
		return task.Task{}, formatError(err)
	}

	return t, nil
}

// List implements [task.Store].
func (s *TaskStore) List(ctx context.Context, f task.ListFilter) ([]task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("list", "", err)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT 
			"id", "title", "description", "status", 
			"due_at", "created_at", "updated_at"
		FROM "tasks" 
		WHERE ($1 = '' OR "status" = $1)
		ORDER BY "id" DESC	
		LIMIT $2 OFFSET $3
		`,
		f.Status,
		f.Limit,
		f.Offset,
	)
	if err != nil {
		return nil, formatError(err)
	}
	defer rows.Close()

	tasks := make([]task.Task, 0)
	for rows.Next() {
		t := task.Task{}
		if err := scanToTask(rows, &t); err != nil {
			return nil, formatError(err)
		}

		tasks = append(tasks, t)
	}

	if err = rows.Err(); err != nil {
		return nil, formatError(err)
	}

	return tasks, nil
}

// Update implements [task.Store].
func (s *TaskStore) Update(ctx context.Context, id uuid.UUID, p task.Patch) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("update", id.String(), err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, formatError(err)
	}
	defer tx.Rollback()

	t, err := s.getForUpdate(ctx, tx, id, formatError)
	if err != nil {
		return t, err
	}

	err = t.Patch(p)
	if err != nil {
		return task.Task{}, err
	}

	res, err := tx.ExecContext(
		ctx,
		`UPDATE "tasks"
		SET
			"title" = $1,
			"description" = $2,
			"status" = $3,
			"due_at" = $4,
			"updated_at" = $5
		WHERE "id" = $6
		`,
		t.Title,
		t.Description,
		t.Status,
		t.DueAt,
		t.UpdatedAt,
		t.ID,
	)
	if err != nil {
		return task.Task{}, formatError(err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return task.Task{}, formatError(err)
	}

	if affected == 0 {
		return task.Task{}, task.ErrNotFound
	}

	err = tx.Commit()
	if err != nil {
		return task.Task{}, formatError(err)
	}

	return t, nil
}

// MarkOverdue implements [task.Store].
func (s *TaskStore) MarkOverdue(ctx context.Context, now time.Time) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("mark overdue", "", err)
	}

	res, err := s.db.ExecContext(
		ctx,
		`UPDATE "tasks"
		SET "status" = 'overdue', "updated_at" = $1
		WHERE "status" NOT IN ('overdue', 'done') AND  "due_at" < $1
		`,
		now,
	)
	if err != nil {
		return 0, formatError(err)
	}

	affected, err := res.RowsAffected()
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
