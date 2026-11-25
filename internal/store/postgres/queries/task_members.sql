-- name: CreateTaskMember :exec
INSERT INTO task_members (task_id, user_id, role, invitation_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetTaskMember :one
SELECT task_id, user_id, role, invitation_id, created_at, updated_at
FROM task_members
WHERE task_id = $1 AND user_id = $2;

-- name: SetTaskMemberRole :one
WITH updated AS (
  UPDATE task_members
  SET role = sqlc.arg('role'), updated_at = sqlc.arg('now')
  WHERE task_id = sqlc.arg('task_id') AND user_id = sqlc.arg('user_id')
  RETURNING user_id, role, created_at
)
SELECT up.user_id, u.email, up.role, up.created_at
FROM updated AS up
INNER JOIN users AS u ON up.user_id = u.id;

-- name: RemoveTaskMember :execrows
DELETE FROM task_members
WHERE task_id = $1 AND user_id = $2;

-- name: CountTaskMembers :one
SELECT COUNT(*)
FROM task_members
WHERE task_id = $1;

-- name: ListTaskShares :many
WITH shares AS (
  SELECT
    'member'::text AS kind,
    tm.user_id     AS id,
    u.email        AS email,
    tm.role        AS role,
    tm.created_at   AS created_at,
    NULL::timestamptz AS expires_at
  FROM task_members AS tm
  INNER JOIN users AS u ON tm.user_id = u.id
  WHERE tm.task_id = sqlc.arg('task_id')

  UNION ALL

  SELECT
    'invitation'::text AS kind,
    ti.id AS id,
    ti.email AS email,
    ti.role AS role,
    ti.created_at AS created_at,
    ti.expires_at AS expires_at
  FROM task_invitations AS ti
  WHERE ti.task_id = sqlc.arg('task_id')
    AND ti.status = 'pending'
    AND ti.expires_at > sqlc.arg('now')
)
SELECT
  s.kind, s.id, s.email, s.role, s.created_at, s.expires_at
FROM shares AS s
ORDER BY s.created_at ASC;
