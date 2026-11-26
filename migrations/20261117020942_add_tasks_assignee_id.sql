-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE tasks
ADD assignee_id uuid NULL;

ALTER TABLE tasks
ADD CONSTRAINT tasks_assignee_id_fkey
FOREIGN KEY (assignee_id) REFERENCES users(id) ON DELETE SET NULL NOT VALID;
