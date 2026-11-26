-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE tasks
VALIDATE CONSTRAINT tasks_assignee_id_fkey;
