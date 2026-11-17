-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS task_invitations (
  id uuid PRIMARY KEY,
  task_id uuid NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  email text NOT NULL,
  role text NOT NULL CHECK (role IN ('viewer', 'contributor', 'editor', 'manager')),
  assign_on_accept boolean NOT NULL DEFAULT false,
  status text NOT NULL CHECK (status IN ('pending', 'accepted', 'declined', 'cancelled')),
  token_hash bytea UNIQUE CHECK (octet_length(token_hash) = 32),
  expires_at timestamptz NOT NULL,
  created_at timestamptz DEFAULT NOW() NOT NULL,
  updated_at timestamptz DEFAULT NOW() NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS task_invitations_task_id_email_pending_idx
ON task_invitations (task_id, email)
WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS task_invitations_expires_at_pending_idx
ON task_invitations (expires_at)
WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS task_invitations_updated_at_closed_idx
ON task_invitations (updated_at)
WHERE status IN ('declined', 'cancelled');
