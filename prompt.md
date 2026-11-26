# Task: move the test suite to Postgres-only and apply the per-layer test criteria

## Context

- Repo: `github.com/dung204/taskapi` (Go 1.27, chi, oapi-codegen strict server, sqlc, goose, pgx, Postgres 17, Redis). Read `CLAUDE.md`, `internal/httpapi/CLAUDE.md` and `internal/task/CLAUDE.md` first.
- Task authorization and sharing (fixed roles `viewer < contributor < editor < manager` plus a virtual owner, invitations, members, assignee, caps) is being implemented **by the repo owner, by hand, spec-first and TDD**. The source of truth is `internal/httpapi/docs/openapi.yml` plus the planning doc "Phân quyền Task: bản kế hoạch" (https://claude.ai/artifact/XEUiSVvvNaaxpvGWwjo4sL, may not be reachable; the repo wins). **Do not implement the feature** unless asked; this task is about the test setup and the test strategy.
- Every store in the app is Postgres-backed except the in-memory `task.Store`, which only exists for tests and learning exercises.

## Goal

1. Remove the in-memory store; all store, service and HTTP tests run against real Postgres.
2. Make `make test` self-sufficient: it starts Postgres when needed.
3. Organise tests by layer following the criteria below.

## Constraints

- **Minimal dependencies.** Standard library plus what is already in `go.mod` (`go-cmp` is the one test-only library). No testcontainers, no assertion or mock libraries. Ask first if something seems to need one.
- Never edit generated code (`internal/httpapi/oapigen`, `sqlcgen`); regenerate with `make gen` / `make gen-sql`.
- Tests truncate tables: never point them at a non-test database. Do not run `docker compose down -v` on the dev project.
- Do not commit or push unless asked. If asked, one concern per commit (a pure rename or removal is its own commit).
- Code, comments and commit messages in English. Talk to the user in Vietnamese.

## Part 1: Infrastructure

1. **Remove the memory store**: `internal/store/memory`, `TASKS_STORE`, the memory branch in `cmd/server/wiring.go`, the config field and its `config_test` cases, `.env.example`, `docker-compose.yml`, and the line about it in `CLAUDE.md`.
2. **Per-package databases.** `go test ./...` runs packages in parallel, so two packages must never truncate the same database. Create and migrate one template database (goose), and let a helper in `internal/testutil` (it must not import domain packages) clone one database per test package in `TestMain` (`CREATE DATABASE ... TEMPLATE ...`) and drop it at the end. A template cannot be cloned while it has connections, so never run tests directly on it.
3. **Isolation inside a package.** Tests create their own users and tasks with unique ids and emails; they must not depend on empty tables, so they can use `t.Parallel()`. Wrapping a test in a rolled-back transaction is not possible because the app opens its own transactions.
4. **Safety guard.** The helper refuses to run if the database name does not contain `test`.
5. **`make test` runs a script** (`scripts/test.sh`):
   - If `TEST_DATABASE_URL` is set, use it and skip Docker (CI has its own Postgres service and keeps using `make ci`).
   - Otherwise start a **separate compose project** (`docker compose -p taskapi-test -f docker-compose.test.yml up -d --wait`). The file contains only `db` (Postgres 17, healthcheck, data on `tmpfs`, a published port different from the dev stack). The dev `docker-compose.yml` is for local dev only and is left alone.
   - Create and migrate the template database, export `TEST_DATABASE_URL`, run `go test`.
   - Leave the container running between runs; add `make test-down` to remove the test project (`-p taskapi-test down -v` is safe because it only touches that project).
   - If Docker is not available, fail with a clear message that also mentions `TEST_DATABASE_URL`.
6. **Missing database behaviour.** Locally a missing `TEST_DATABASE_URL` may `t.Skip`; in CI (`CI` env var set) it must **fail**, so `make ci` cannot go green without running these tests.
7. **Only keep fakes for non-Postgres things**: the clock, rate limiters, mailer, and thin decorators around the real store that inject errors (to test 500/503 and fail-open paths).
8. `tasktest.TestStore` existed so memory and Postgres could share one suite. With one implementation, fold it into the `postgres` package tests (update `CLAUDE.md` and the coverage exclusions in the `Makefile` accordingly). Update `lefthook.yml` and `CLAUDE.md` for the new `make test`.
9. Add one regression test with the pool limited to `MaxConns = 1` for `AcceptInvitation` (it must finish; a nested connection acquisition would hang).

## Part 2: Test criteria per layer

Principle: **test each behaviour at the lowest layer that can observe it**; a higher layer only tests what it alone sees. Do not repeat the same condition in two layers (the HTTP layer needs just one case showing that the error maps to the right status and message).

| Layer | What it tests | Done when |
|---|---|---|
| Pure (no DB) | `rolePerms` / `Can`, `RequiredForPatch`, constructors (`NewInvitation`, `NewMember`), `Invitation.IsPending`, `email.Parse`, display rounding of the invitation lifetime | table tests with boundaries (exactly at expiry, upper case, whitespace, empty patch); every role including the owner has a row |
| Store (`postgres` package, real DB, direct, sequential) | each method: business errors and constraint-name mapping, time boundaries (`expires_at > now`, `updated_at < before`), filtering by viewer and by `task_id`, `ListShares` ordering, cleanup jobs, `GetVerifiedUserIDByEmail` (unverified, disabled, upper case) | every error listed in an interface comment has a test; security-relevant SQL has a direct test |
| `Service` (real DB; fake limiter, mailer, clock) | every branch (both `Invite` branches, enough or missing permission, user found or not), every business error, boundaries (49/50 members, 199/200 pending invitations), side effects, **races** | every branch, cap and race listed below has a test; branch coverage alone is not enough, boundary values must be present |
| `httpapi` (real DB; same fakes) | status code and message mapping, JSON shape, headers (`Retry-After`, `WWW-Authenticate`), order of checks, bad parameters, a test generated from `rolePerms`, one main flow per endpoint | every (operation, status code) pair in `openapi.yml` has at least one test, with the message pinned to the spec `example` |

When a test fails, its layer should point to the cause: `httpapi` means mapping or pipeline, `Service` means logic or locking, store means SQL.

## Part 3: Scenarios that must be covered (by layer)

**Order of checks** (`httpapi`): 401, 429 (IP), path/body decode 400, 404 (cannot see the task), 403 (lacks the permission), value errors (400/422/409), then the hourly owner cap (429). 404 means the caller cannot see the task; 403 means they can see it but lack the flag.

**Permissions** (`httpapi` generated from the matrix, plus pure tests): a `PATCH` missing both edit and status permission reports the edit-content error and increments the `edit_content` metric once.

**Invite** (`Service`):
- A verified account is added directly (mail `task_added`); no account or unverified or disabled account gets a pending invitation (mail `task_invitation`).
- Inviting yourself: 422. Already a member: 409. Pending invitation for the same email: 409; an **expired** pending row does not block a new invitation (the expired row is deleted first).
- Role must be grantable (422), checked before lookups and limiters. Rejected requests must not consume limiter budget.
- Caps: 50 members per task (422 on invite, counted again under the task row lock; **409 on accept**), 200 pending invitations per owner (422, soft cap, not serialised), 30 invitations per hour per owner (429 with `Retry-After`, Redis, fail-open).
- A per-recipient mail limiter that blocks still lets the invitation succeed, with no outbox row.
- `assign: true` on direct add sets the assignee in the same transaction.

**AcceptInvitation / Decline** (`Service`, `httpapi`): every failure (unknown token, expired, closed, wrong account) returns the same 404 `invitation not found or expired`, except a full task (409). The caller's verified email must match the invited email. A malformed token is 400.

**CancelInvitation** (store and `Service`): the `UPDATE` filters by `task_id`, so an invitation of another task is 404.

**ListShares** (store, `Service`, `httpapi`): members plus unexpired pending invitations, ordered by `created_at, id`; a stranger gets 404, never an empty list; no shares gives `[]`, not `null`.

**Assign / RemoveMember / Leave**: the assignee must be the owner or a member; removing a member clears their assignment in the same transaction; `Assign` racing with `RemoveMember` never leaves a non-member as assignee.

**Races** (`Service`, real Postgres, two goroutines): last free member slot, `Assign` versus `RemoveMember`, `Accept` versus a direct-add `Invite` of the same person.

**Cleanup jobs** (store): `DeleteClosedBefore` must not delete an `accepted` invitation that a member still references.

**Outbox / mail** (`task.EmailBuilder`): `ErrNothingToSend` when the row is gone, or the invitation is no longer pending or has expired; any other store error is returned (so the worker retries); a missing `RefID` for a task kind is a plain error, not `ErrNothingToSend`; rebuilding a mail invalidates the previous token; if storing the token hash fails, no message is returned.

**Metrics**: `task_authz_denied_total{permission}` and `task_invitations_total{event}` (`created`, `added`, `accepted`, `declined`, `cancelled`, `expired`) increase only after the transaction commits.

## Open decisions: ask the user, do not guess

- Does a direct add cancel an existing pending invitation for the same email? (Also decides the lock order between `Accept` and `Invite`.)
- Default values for `INVITE_MAIL_COOLDOWN` and `INVITE_MAIL_HOURLY_MAX` (current thinking: about 10s and 5); `INVITE_OWNER_HOURLY_MAX` defaults to 30.
- Whether to keep the jsonb domain trick for `ListShares`, and whether to adopt the pgx `timestamptz` UTC hook.

## Definition of done

- `make ci` passes locally and in CI with no skipped Postgres tests.
- No reference to the memory store remains (code, config, docs).
- `CLAUDE.md`, `.env.example`, `docker-compose.yml`, `lefthook.yml` and the `Makefile` reflect the new flow.
- Report back (in Vietnamese): what changed, what was left undone, and any decision you had to ask about.
