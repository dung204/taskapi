-- +goose Up
CREATE TABLE IF NOT EXISTS tasks (
  id uuid PRIMARY KEY,
  title text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
  description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 2000),
  status text NOT NULL CHECK(status IN ('todo', 'doing', 'done', 'overdue')),
  due_at timestamptz,
  created_at timestamptz DEFAULT NOW() NOT NULL,
  updated_at timestamptz DEFAULT NOW() NOT NULL
);
