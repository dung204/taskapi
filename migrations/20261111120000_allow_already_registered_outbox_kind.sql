-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

-- The constraint is re-added right below in the same transaction, so there is
-- no window without the guarantee.
ALTER TABLE email_outbox
-- squawk-ignore ban-drop-constraint
DROP CONSTRAINT IF EXISTS email_outbox_kind_check;

-- Validated in the next migration so the table is not scanned under this lock.
ALTER TABLE email_outbox
ADD CONSTRAINT email_outbox_kind_check
CHECK (kind IN ('verify_email', 'reset_password', 'already_registered')) NOT VALID;
