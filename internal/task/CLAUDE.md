# internal/task

- **Time precision**: timestamps are normalized to UTC and truncated to microseconds (what Postgres keeps) in `task.New` / `Task.Patch`. Tests compare times exactly, so build expected values the same way.
- **IDs and ordering**: IDs are UUIDv7. `List` returns tasks ordered by `id` DESC (newest first) in both stores, and must return a non-nil empty slice so the API responds `[]`, not `null`.
- **`Task.Patch` is atomic**: it works on a copy and only commits when every field is valid. `updated_at` moves only when a field really changes.
- **`overdue` is system-set**: it is valid for filtering (`GET /tasks?status=overdue`) but rejected in a patch or create. Only `MarkOverdue` sets it, and it skips `done` tasks and tasks without `due_at`.
- **Every store test goes in `tasktest.TestStore`** so memory and Postgres stay in sync.
- **Test style**: table-driven, using the `tasktest` checkers. `CheckTask` / `CheckPatch` take `go-cmp` options, so to skip a field pass `cmpopts.IgnoreFields(task.Task{}, "UpdatedAt")` rather than adding a flag or equalizing the values by hand.
