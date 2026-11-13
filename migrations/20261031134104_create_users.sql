-- +goose Up
CREATE TABLE IF NOT EXISTS users (
  id uuid PRIMARY KEY,
  email text NOT NULL UNIQUE CHECK (email = lower(email)),
  password_hash text,
  email_verified_at timestamptz,
  disabled_at timestamptz,
  tokens_valid_after timestamptz,
  created_at timestamptz NOT NULL DEFAULT NOW(),
  updated_at timestamptz NOT NULL DEFAULT NOW()
);

INSERT INTO users (id, email)
VALUES ('00000000-0000-0000-0000-000000000000', 'legacy@system.invalid');
