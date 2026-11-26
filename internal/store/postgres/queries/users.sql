-- name: CreateUser :exec
INSERT INTO users
  (id, email, password_hash, email_verified_at, disabled_at,
  tokens_valid_after, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetUserByEmail :one
SELECT
  id, email, password_hash, email_verified_at, disabled_at,
  tokens_valid_after, created_at, updated_at
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT
  id, email, password_hash, email_verified_at, disabled_at,
  tokens_valid_after, created_at, updated_at
FROM users
WHERE id = $1;

-- name: SetUserEmailVerified :execrows
UPDATE users
SET email_verified_at = $1,
    updated_at = $2
WHERE id = $3;

-- name: UpdateUserPassword :execrows
UPDATE users
SET password_hash = sqlc.arg('password_hash'),
    tokens_valid_after = GREATEST(tokens_valid_after, sqlc.arg('at')),
    updated_at = sqlc.arg('at')
WHERE id = sqlc.arg('id');

-- name: SetTokensValidAfterForUser :execrows
UPDATE users
SET tokens_valid_after = GREATEST(tokens_valid_after, sqlc.arg('at')),
    updated_at = sqlc.arg('at')
WHERE id = sqlc.arg('id');

-- name: GetVerifiedUserIDByEmail :one
SELECT id
FROM users
WHERE email = $1
  AND email_verified_at IS NOT NULL
  AND disabled_at IS NULL;
