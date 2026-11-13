-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE email_outbox
VALIDATE CONSTRAINT email_outbox_kind_check;

-- +goose Down
SELECT 1;
