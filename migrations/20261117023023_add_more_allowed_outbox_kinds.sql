-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE email_outbox
-- squawk-ignore ban-drop-constraint
DROP CONSTRAINT IF EXISTS email_outbox_kind_check;

ALTER TABLE email_outbox
ADD CONSTRAINT email_outbox_kind_check
CHECK (kind IN ('verify_email', 'reset_password', 'already_registered', 'task_invitation', 'task_added')) NOT VALID;
