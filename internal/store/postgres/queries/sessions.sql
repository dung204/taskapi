-- name: CreateSession :exec
INSERT INTO sessions
  (id, user_id, refresh_token_hash, previous_refresh_token_hash,
  rotated_at, expires_at, created_at, user_agent)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetSessionByRefreshTokenHash :one
SELECT
  id, user_id, refresh_token_hash, previous_refresh_token_hash,
  rotated_at, expires_at, created_at, user_agent
FROM sessions
WHERE refresh_token_hash = $1 OR previous_refresh_token_hash = $1;

-- name: RotateSession :execrows
UPDATE sessions
SET previous_refresh_token_hash = refresh_token_hash,
    refresh_token_hash = sqlc.arg('newHash'),
    rotated_at = sqlc.arg('now')
WHERE id = sqlc.arg('id') AND refresh_token_hash = sqlc.arg('oldHash') AND expires_at > sqlc.arg('now');

-- name: DeleteSessionByID :execrows
DELETE FROM sessions
WHERE id = $1;

-- name: DeleteSessionsByUserID :execrows
DELETE FROM sessions
WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE id IN (
  SELECT id FROM sessions AS s
  WHERE s.expires_at <= sqlc.arg('now')
  LIMIT sqlc.arg('limit')
);

