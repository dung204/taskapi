package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

type Scannable interface {
	Scan(dest ...any) error
}

func scanToTask(s Scannable, t *task.Task) error {
	return s.Scan(
		&t.ID,
		&t.Title,
		&t.Description,
		&t.Status,
		t.DueAt,
		&t.CreatedAt,
		&t.UpdatedAt,
	)
}

type TaskStore struct {
	db *sql.DB
}

func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{
		db,
	}
}

// Create implements [task.Store].
func (store *TaskStore) Create(ctx context.Context, t task.Task) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("create", t.ID.String(), err)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, formatError(err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO "tasks" VALUES ($1,$2,$3,$4,$5,$6,$7)`,
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

	err = tx.Commit()
	if err != nil {
		return task.Task{}, formatError(err)
	}

	return t, nil
}

// Delete implements [task.Store].
func (store *TaskStore) Delete(ctx context.Context, id uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("delete", id.String(), err)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return formatError(err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(
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
func (store *TaskStore) Get(ctx context.Context, id uuid.UUID) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", id.String(), err)
	}

	return store.get(ctx, id, formatError)
}

func (store *TaskStore) get(ctx context.Context, id uuid.UUID, formatError func(err error) error) (task.Task, error) {
	t := task.Task{}
	row := store.db.QueryRowContext(
		ctx,
		`SELECT 
			"id", "title", "description", "status", 
			"due_at", "created_at", "updated_at"
		FROM "tasks" 
		WHERE "id" = $1`,
		id,
	)
	err := scanToTask(row, &t)

	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, task.ErrNotFound
	}

	if err != nil {
		return task.Task{}, formatError(err)
	}

	return t, nil
}

// List implements [task.Store].
func (store *TaskStore) List(ctx context.Context, f task.ListFilter) ([]task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "", err)
	}

	rows, err := store.db.QueryContext(
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

	return tasks, nil
}

// Update implements [task.Store].
func (store *TaskStore) Update(ctx context.Context, id uuid.UUID, p task.Patch) (task.Task, error) {
	formatError := func(err error) error {
		return formatPostgresError("update", id.String(), err)
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return task.Task{}, formatError(err)
	}
	defer tx.Rollback()

	t, err := store.get(ctx, id, formatError)
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
			"title" = $1
			"description" = $2
			"status" = $3
			"due_at" = $4
			"updated_at" = $5
		WHERE "id" = $6
		`,
		t.Title,
		t.Description,
		t.Status,
		t.DueAt,
		t.CreatedAt,
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

	return t, nil
}

func formatPostgresError(op string, taskId string, err error) error {
	id := ""
	if taskId != "" {
		id = " " + taskId
	}

	return fmt.Errorf("postgres: %s task%s: %w", op, id, err)
}
