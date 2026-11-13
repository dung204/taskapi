-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS sessions (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash bytea NOT NULL UNIQUE CHECK(octet_length(refresh_token_hash) = 32),
  previous_refresh_token_hash bytea CHECK(octet_length(previous_refresh_token_hash) = 32),
  rotated_at timestamptz,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  user_agent text
);

CREATE INDEX IF NOT EXISTS sessions_prev_refresh_token_hash_idx
ON sessions (previous_refresh_token_hash)
WHERE previous_refresh_token_hash IS NOT NULL;

CREATE INDEX IF NOT EXISTS sessions_user_id_idx
ON sessions (user_id);

CREATE INDEX IF NOT EXISTS sessions_expires_at_idx
ON sessions (expires_at);
