// Command seed fills a development database with sample data for every table.
//
// It first TRUNCATEs every table (and restores the legacy user that the
// users migration inserts), so ALL existing data is lost. Never point it at a
// database you care about. Running it twice gives the same result. Apply the
// migrations first (make migrate-up). Every seeded user has the password
// printed below.
package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/auth"
)

const password = "Password123!"

// Fixed IDs, so that re-running the seed is a no-op.
const (
	alice = "10000000-0000-0000-0000-000000000001"
	bob   = "10000000-0000-0000-0000-000000000002"
	carol = "10000000-0000-0000-0000-000000000003"
	dave  = "10000000-0000-0000-0000-000000000004" // unverified
	erin  = "10000000-0000-0000-0000-000000000005" // disabled

	task1 = "20000000-0000-0000-0000-000000000001"
	task2 = "20000000-0000-0000-0000-000000000002"
	task3 = "20000000-0000-0000-0000-000000000003"
	task4 = "20000000-0000-0000-0000-000000000004"
	task5 = "20000000-0000-0000-0000-000000000005"

	invAccepted  = "30000000-0000-0000-0000-000000000001"
	invAccepted2 = "30000000-0000-0000-0000-000000000002"
	invPending   = "30000000-0000-0000-0000-000000000003"
	invDeclined  = "30000000-0000-0000-0000-000000000004"
	invCancelled = "30000000-0000-0000-0000-000000000005"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dbURL := flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres URL (default: $DATABASE_URL)")
	flag.Parse()
	if *dbURL == "" {
		return errors.New("DATABASE_URL (or -database-url) is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pool, err := pgxpool.New(ctx, *dbURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	hasher := auth.NewArgon2Hasher(auth.Argon2HasherOpts{Time: 1, MemoryKiB: 64 * 1024, Threads: 4, Concurrency: 1})
	hash, err := hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := seed(ctx, tx, hash, time.Now().UTC()); err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	fmt.Printf("Seeded. Users: alice@example.com, bob@example.com, carol@example.com, dave@example.com (unverified), erin@example.com (disabled); password %q\n", password)

	return nil
}

func digest(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func seed(ctx context.Context, tx pgx.Tx, pwHash string, now time.Time) error {
	day := 24 * time.Hour
	var firstErr error
	exec := func(sql string, args ...any) {
		if firstErr != nil {
			return
		}
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			firstErr = fmt.Errorf("%.60s...: %w", sql, err)
		}
	}

	// Wipe everything. CASCADE covers the foreign keys; the legacy user the
	// users migration inserted is put back so the database matches a fresh
	// migration.
	exec(`TRUNCATE users, sessions, user_tokens, email_outbox, tasks, task_invitations, task_members CASCADE`)
	exec(`INSERT INTO users (id, email) VALUES ('00000000-0000-0000-0000-000000000000', 'legacy@system.invalid')`)

	// users
	for _, u := range []struct {
		id, email             string
		verified, disabledNow bool
	}{
		{alice, "alice@example.com", true, false},
		{bob, "bob@example.com", true, false},
		{carol, "carol@example.com", true, false},
		{dave, "dave@example.com", false, false},
		{erin, "erin@example.com", true, true},
	} {
		var verifiedAt, disabledAt *time.Time
		if u.verified {
			verifiedAt = &now
		}
		if u.disabledNow {
			disabledAt = &now
		}
		exec(`INSERT INTO users (id, email, password_hash, email_verified_at, disabled_at)
		      VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
			u.id, u.email, pwHash, verifiedAt, disabledAt)
	}

	// sessions
	exec(`INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at, user_agent)
	      VALUES ('40000000-0000-0000-0000-000000000001', $1, $2, $3, 'seed/curl')
	      ON CONFLICT DO NOTHING`, alice, digest("seed-refresh-alice"), now.Add(30*day))
	exec(`INSERT INTO sessions (id, user_id, refresh_token_hash, previous_refresh_token_hash, rotated_at, expires_at, user_agent)
	      VALUES ('40000000-0000-0000-0000-000000000002', $1, $2, $3, $4, $5, 'seed/browser')
	      ON CONFLICT DO NOTHING`, bob, digest("seed-refresh-bob-new"), digest("seed-refresh-bob-old"), now.Add(-time.Hour), now.Add(30*day))
	exec(`INSERT INTO sessions (id, user_id, refresh_token_hash, expires_at, user_agent)
	      VALUES ('40000000-0000-0000-0000-000000000003', $1, $2, $3, 'seed/expired')
	      ON CONFLICT DO NOTHING`, carol, digest("seed-refresh-carol-expired"), now.Add(-day))

	// user_tokens
	exec(`INSERT INTO user_tokens (id, user_id, purpose, token_hash, expires_at)
	      VALUES ('50000000-0000-0000-0000-000000000001', $1, 'verify_email', $2, $3)
	      ON CONFLICT DO NOTHING`, dave, digest("seed-verify-dave"), now.Add(day))
	exec(`INSERT INTO user_tokens (id, user_id, purpose, token_hash, expires_at, used_at)
	      VALUES ('50000000-0000-0000-0000-000000000002', $1, 'reset_password', $2, $3, $4)
	      ON CONFLICT DO NOTHING`, alice, digest("seed-reset-alice"), now.Add(time.Hour), now.Add(-time.Hour))

	// tasks
	for _, t := range []struct {
		id, title, desc, status, owner string
		assignee                       *string
		due                            *time.Time
	}{
		{task1, "Write project proposal", "Draft and circulate the proposal.", "doing", alice, new(bob), new(now.Add(3 * day))},
		{task2, "Fix login bug", "Users cannot log in with a trailing space in the email.", "todo", alice, nil, new(now.Add(day))},
		{task3, "Ship v1.0", "", "done", bob, new(bob), nil},
		{task4, "Renew certificates", "Missed the deadline.", "overdue", bob, nil, new(now.Add(-2 * day))},
		{task5, "Plan team offsite", "Venue, dates, budget.", "todo", carol, new(carol), new(now.Add(14 * day))},
	} {
		exec(`INSERT INTO tasks (id, title, description, status, due_at, owner_id, assignee_id)
		      VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
			t.id, t.title, t.desc, t.status, t.due, t.owner, t.assignee)
	}

	// task_invitations: one per status
	for _, i := range []struct {
		id, task, email, role, status string
		assign                        bool
		token                         []byte
		expires                       time.Time
	}{
		{invAccepted, task1, "bob@example.com", "editor", "accepted", true, nil, now.Add(7 * day)},
		{invAccepted2, task1, "carol@example.com", "viewer", "accepted", false, nil, now.Add(7 * day)},
		{invPending, task2, "dave@example.com", "contributor", "pending", false, digest("seed-invite-pending"), now.Add(7 * day)},
		{invDeclined, task3, "alice@example.com", "manager", "declined", false, nil, now.Add(7 * day)},
		{invCancelled, task4, "carol@example.com", "viewer", "cancelled", false, nil, now.Add(7 * day)},
	} {
		exec(`INSERT INTO task_invitations (id, task_id, email, role, assign_on_accept, status, token_hash, expires_at)
		      VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT DO NOTHING`,
			i.id, i.task, i.email, i.role, i.assign, i.status, i.token, i.expires)
	}

	// task_members: created by the accepted invitations
	exec(`INSERT INTO task_members (task_id, user_id, role, invitation_id) VALUES
	      ($1, $2, 'editor', $3), ($1, $4, 'viewer', $5) ON CONFLICT DO NOTHING`,
		task1, bob, invAccepted, carol, invAccepted2)

	// email_outbox: one row per kind, one of them still waiting to be sent
	for i, o := range []struct {
		kind, to string
		user     string
		sent     bool
	}{
		{"verify_email", "dave@example.com", dave, false},
		{"reset_password", "alice@example.com", alice, true},
		{"already_registered", "bob@example.com", bob, true},
		{"task_invitation", "dave@example.com", dave, true},
		{"task_added", "bob@example.com", bob, true},
	} {
		var sentAt *time.Time
		if o.sent {
			sentAt = &now
		}
		exec(`INSERT INTO email_outbox (id, user_id, kind, to_email, attempts, next_attempt_at, sent_at)
		      VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
			fmt.Sprintf("60000000-0000-0000-0000-%012d", i+1), o.user, o.kind, o.to, btoi(o.sent), now, sentAt)
	}
	// a failed one
	exec(`INSERT INTO email_outbox (id, user_id, kind, to_email, attempts, next_attempt_at, failed_at, last_error)
	      VALUES ('60000000-0000-0000-0000-000000000099', $1, 'verify_email', 'carol@example.com', 5, $2, $2, 'smtp: connection refused')
	      ON CONFLICT DO NOTHING`, carol, now)

	return firstErr
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
