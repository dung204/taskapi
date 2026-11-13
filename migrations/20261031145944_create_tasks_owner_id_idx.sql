-- +goose NO TRANSACTION
-- +goose Up
-- squawk-disable-assume-in-transaction

SET lock_timeout = '5s';
SET statement_timeout = '30s';

DROP INDEX CONCURRENTLY IF EXISTS tasks_owner_id_idx;

CREATE INDEX CONCURRENTLY IF NOT EXISTS tasks_owner_id_idx
ON tasks (owner_id);

RESET lock_timeout;
RESET statement_timeout;
