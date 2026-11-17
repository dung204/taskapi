-- +goose NO TRANSACTION
-- +goose Up
-- squawk-disable-assume-in-transaction

SET lock_timeout = '5s';
SET statement_timeout = '30s';

DROP INDEX CONCURRENTLY IF EXISTS tasks_assignee_id_idx;

CREATE INDEX CONCURRENTLY IF NOT EXISTS tasks_assignee_id_idx
ON tasks (assignee_id)
WHERE assignee_id IS NOT NULL;

RESET lock_timeout;
RESET statement_timeout;
