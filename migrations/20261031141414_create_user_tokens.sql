-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS user_tokens (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose text NOT NULL CHECK(purpose IN ('verify_email', 'reset_password')),
  token_hash bytea NOT NULL UNIQUE CHECK(octet_length(token_hash) = 32),
  expires_at timestamptz NOT NULL,
  used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_user_tokens_user_id_purpose
ON user_tokens (user_id, purpose);

CREATE INDEX IF NOT EXISTS idx_user_tokens_expires_at
ON user_tokens (expires_at);
