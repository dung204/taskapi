-- name: CreateUserToken :exec
INSERT INTO user_tokens (id, user_id, purpose, token_hash, expires_at, used_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ConsumeUserToken :one
UPDATE user_tokens
SET used_at = sqlc.arg('now')
WHERE token_hash = sqlc.arg('token_hash')
  AND purpose = sqlc.arg('purpose')
  AND used_at IS NULL
  AND expires_at > sqlc.arg('now')
RETURNING user_id;

-- name: DeleteUserTokensByUserAndPurpose :exec
DELETE FROM user_tokens
WHERE user_id = $1 AND purpose = $2;

-- name: DeleteExpiredUserTokens :execrows
DELETE FROM user_tokens
WHERE id IN (
  SELECT id FROM user_tokens AS ut
  WHERE ut.expires_at <= sqlc.arg('now')
  LIMIT sqlc.arg('limit')
);
