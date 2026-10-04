CREATE TABLE "tasks" (
  "id" UUID PRIMARY KEY,
  "title" VARCHAR(200) NOT NULL,
  "description" VARCHAR(2000) DEFAULT '' NOT NULL,
  "status" VARCHAR NOT NULL CHECK("status" IN ('todo', 'doing', 'done', 'overdue')),
  "due_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ DEFAULT NOW() NOT NULL,
  "updated_at" TIMESTAMPTZ DEFAULT NOW() NOT NULL
);