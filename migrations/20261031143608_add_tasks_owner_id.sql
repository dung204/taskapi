-- +goose Up
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '30s';

ALTER TABLE tasks
ADD owner_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE tasks
ADD CONSTRAINT tasks_owner_id_fkey
FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;

-- remove column default for better understanding
ALTER TABLE tasks
-- squawk-ignore ban-drop-default
ALTER COLUMN owner_id DROP DEFAULT;
