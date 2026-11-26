-- name: CreateTaskInvitation :exec
INSERT INTO task_invitations
  (id, task_id, email, role, assign_on_accept, status,
  token_hash, expires_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetTaskInvitationByTokenHash :one
SELECT
  id, task_id, email, role, assign_on_accept, status,
  token_hash, expires_at, created_at, updated_at
FROM task_invitations
WHERE token_hash = $1
FOR UPDATE;

-- name: GetTaskInvitationForMail :one
SELECT
  sqlc.embed(ti),
  t.title AS task_title,
  u.email AS owner_email
FROM task_invitations AS ti
INNER JOIN tasks AS t ON ti.task_id = t.id
INNER JOIN users AS u ON t.owner_id = u.id
WHERE ti.id = $1;

-- name: SetTaskInvitationStatus :execrows
UPDATE task_invitations
SET status = $1,
    updated_at = $2
WHERE id = $3;

-- name: SetTaskInvitationTokenHash :execrows
UPDATE task_invitations
SET token_hash = $1,
    updated_at = $2
WHERE id = $3;

-- name: DeleteExpiredPendingTaskInvitationsFor :execrows
DELETE FROM task_invitations
WHERE task_id = sqlc.arg('task_id')
  AND email = sqlc.arg('email')
  AND status = 'pending'
  AND expires_at <= sqlc.arg('now');

-- name: CountPendingTaskInvitationsByOwner :one
SELECT COUNT(ti.id)
FROM task_invitations AS ti
INNER JOIN tasks AS t ON ti.task_id = t.id
WHERE t.owner_id = sqlc.arg('owner')
  AND ti.status = 'pending';

-- name: DeleteExpiredPendingTaskInvitations :execrows
DELETE FROM task_invitations
WHERE id IN (
  SELECT id FROM task_invitations AS ti
  WHERE ti.status = 'pending'
    AND ti.expires_at <= sqlc.arg('now')
  LIMIT sqlc.arg('limit')
);

-- name: DeleteClosedTaskInvitations :execrows
DELETE FROM task_invitations
WHERE id IN (
  SELECT id FROM task_invitations AS ti
  WHERE ti.status IN ('declined', 'cancelled')
    AND ti.expires_at <= sqlc.arg('now')
  LIMIT sqlc.arg('limit')
);

-- name: GetPendingTaskInvitation :one
SELECT id, task_id, email, role, assign_on_accept, status,
  token_hash, expires_at, created_at, updated_at
FROM task_invitations
WHERE task_id = $1
  AND email = $2
  AND status = 'pending'
  AND expires_at > $3;

-- name: CancelTaskInvitation :execrows
UPDATE task_invitations
SET status = 'cancelled', updated_at = $3
WHERE id = $2 AND task_id = $1
  AND status = 'pending' AND expires_at > $3;
