-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

CREATE TABLE IF NOT EXISTS email_outbox (
  id uuid PRIMARY KEY,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind text NOT NULL CHECK(kind IN ('verify_email', 'reset_password')),
  to_email text NOT NULL,
  -- attempts would not be that big to reach the int limit
  -- squawk-ignore prefer-bigint-over-int
  attempts integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL,
  locked_until timestamptz,
  sent_at timestamptz,
  failed_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS email_outbox_ready_idx
ON email_outbox (next_attempt_at)
WHERE sent_at IS NULL AND failed_at IS NULL;

-- +goose StatementBegin
CREATE FUNCTION email_outbox_notify() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('email_outbox', '');
  RETURN NULL;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER email_outbox_notify_trg
AFTER INSERT ON email_outbox
FOR EACH STATEMENT EXECUTE FUNCTION email_outbox_notify();
