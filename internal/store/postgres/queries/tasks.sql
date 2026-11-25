-- name: CreateTask :exec
INSERT INTO tasks (id, title, description, status, due_at, created_at, updated_at, owner_id, assignee_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetTask :one
SELECT
  sqlc.embed(t),
  CASE WHEN t.owner_id = sqlc.arg('viewer') THEN 'owner' ELSE tm.role END::text AS role,
  u.email AS assignee_email
FROM tasks AS t
LEFT JOIN task_members AS tm
  ON t.id = tm.task_id AND tm.user_id = sqlc.arg('viewer')
LEFT JOIN users AS u ON t.assignee_id = u.id
WHERE t.id = sqlc.arg('id')
  AND (t.owner_id = sqlc.arg('viewer') OR tm.user_id IS NOT NULL);

-- name: GetTaskForUpdate :one
SELECT id, title, description, status, due_at, created_at, updated_at, owner_id, assignee_id
FROM tasks
WHERE id = $1
FOR UPDATE;

-- name: ListTasks :many
SELECT
  sqlc.embed(t),
  CASE WHEN t.owner_id = sqlc.arg('viewer') THEN 'owner' ELSE tm.role END::text AS role,
  u.email AS assignee_email
FROM tasks AS t
LEFT JOIN task_members AS tm
  ON t.id = tm.task_id AND tm.user_id = sqlc.arg('viewer')
LEFT JOIN users AS u ON t.assignee_id = u.id
WHERE
  CASE sqlc.arg('scope')::text
    WHEN 'owned' THEN t.owner_id = sqlc.arg('viewer')
    WHEN 'shared' THEN tm.user_id IS NOT NULL
    ELSE (t.owner_id = sqlc.arg('viewer') OR tm.user_id IS NOT NULL)
  END
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status'))
  AND (NOT sqlc.arg('assigned_to_me')::boolean OR t.assignee_id = sqlc.arg('viewer'))
ORDER BY t.id DESC
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

-- name: SetTaskAssignee :execrows
UPDATE tasks
SET assignee_id = $1
WHERE id = $2;

-- name: ClearTaskAssignee :execrows
UPDATE tasks
SET assignee_id = NULL
WHERE id = $1;

-- name: ClearTaskAssigneeIf :exec
UPDATE tasks
SET assignee_id = NULL
WHERE id = $1 AND assignee_id = $2;
