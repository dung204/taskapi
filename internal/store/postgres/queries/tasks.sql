-- name: CreateTask :exec
INSERT INTO tasks (id, title, description, status, due_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetTask :one
SELECT id, title, description, status, due_at, created_at, updated_at
FROM tasks
WHERE id = $1;

-- name: GetTaskForUpdate :one
SELECT id, title, description, status, due_at, created_at, updated_at
FROM tasks
WHERE id = $1
FOR UPDATE;

-- name: ListTasks :many
SELECT id, title, description, status, due_at, created_at, updated_at
FROM tasks
ORDER BY id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: ListTasksByStatus :many
SELECT id, title, description, status, due_at, created_at, updated_at
FROM tasks
WHERE status = sqlc.arg('status')
ORDER BY id DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: UpdateTask :execrows
UPDATE tasks
SET title = $1,
    description = $2,
    status = $3,
    due_at = $4,
    updated_at = $5
WHERE id = $6;

-- name: DeleteTask :execrows
DELETE FROM tasks
WHERE id = $1;

-- name: MarkOverdue :execrows
UPDATE tasks
SET status = 'overdue',
    updated_at = sqlc.arg('now')
WHERE status NOT IN ('overdue', 'done')
  AND due_at < sqlc.arg('now');
