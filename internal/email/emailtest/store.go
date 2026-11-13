// Package emailtest holds test helpers shared by the email store
// implementations, including the behaviour suite that every [email.OutboxStore]
// must pass.
package emailtest

import (
	"context"
	"testing"
	"time"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/email"
)

var base = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

const (
	lease = 5 * time.Minute
	tick  = time.Microsecond // the smallest step Postgres can tell apart
)

// TestOutboxStore runs the outbox behaviour suite. The store has no read
// method, so the suite observes items through Claim and DeleteSent. newStore
// also returns a [auth.UserStore] over the same data, because outbox items
// belong to users and a real backend rejects an item whose user does not exist.
func TestOutboxStore(t *testing.T, newStore func(t *testing.T) (email.OutboxStore, auth.UserStore)) {
	ctx := context.Background()

	// seed creates a verify-email item that becomes due at dueAt.
	seed := func(t *testing.T, s email.OutboxStore, u auth.User, dueAt time.Time) email.OutboxItem {
		t.Helper()

		item, err := email.NewOutboxItem(email.NewOutboxItemParams{
			UserID: u.ID, Kind: email.KindVerifyEmail, ToEmail: u.Email,
		}, base)
		if err != nil {
			t.Fatalf("email.NewOutboxItem: %v", err)
		}
		item.NextAttemptAt = dueAt

		if err := s.Create(ctx, item); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return item
	}

	// claimed is what Claim must return for item after attempts claims.
	claimed := func(item email.OutboxItem, attempts int32) email.ClaimedItem {
		return email.ClaimedItem{
			ID: item.ID, UserID: item.UserID, Kind: item.Kind,
			ToEmail: item.ToEmail, Attempts: attempts,
		}
	}

	claim := func(t *testing.T, s email.OutboxStore, now time.Time, limit int32) []email.ClaimedItem {
		t.Helper()

		got, err := s.Claim(ctx, now, lease, limit)
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		return got
	}

	// Far enough ahead that every lease taken in a test has run out.
	later := base.Add(24 * time.Hour)

	t.Run("Claim returns a due item", func(t *testing.T) {
		tests := []email.Kind{email.KindVerifyEmail, email.KindResetPassword}

		for _, kind := range tests {
			t.Run(string(kind), func(t *testing.T) {
				s, us := newStore(t)
				u := authtest.SeedUser(ctx, t, us)
				item, err := email.NewOutboxItem(email.NewOutboxItemParams{
					UserID: u.ID, Kind: kind, ToEmail: u.Email,
				}, base)
				if err != nil {
					t.Fatalf("email.NewOutboxItem: %v", err)
				}
				if err := s.Create(ctx, item); err != nil {
					t.Fatalf("Create: %v", err)
				}

				CheckClaimed(t, claim(t, s, base, 10), []email.ClaimedItem{claimed(item, 1)})
			})
		}
	})

	t.Run("Claim returns nothing when the outbox is empty", func(t *testing.T) {
		s, _ := newStore(t)

		CheckClaimed(t, claim(t, s, base, 10), nil)
	})

	t.Run("Claim skips items that are not due yet", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base.Add(time.Hour))

		CheckClaimed(t, claim(t, s, base.Add(time.Hour-tick), 10), nil)
		// Due exactly now counts as due.
		CheckClaimed(t, claim(t, s, base.Add(time.Hour), 10), []email.ClaimedItem{claimed(item, 1)})
	})

	t.Run("Claim leases the items it returns", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base)

		CheckClaimed(t, claim(t, s, base, 10), []email.ClaimedItem{claimed(item, 1)})

		// Still leased, up to and including the moment the lease ends.
		CheckClaimed(t, claim(t, s, base.Add(time.Minute), 10), nil)
		CheckClaimed(t, claim(t, s, base.Add(lease), 10), nil)

		// An abandoned item comes back, and the attempt is counted.
		CheckClaimed(t, claim(t, s, base.Add(lease+tick), 10), []email.ClaimedItem{claimed(item, 2)})
	})

	t.Run("Claim takes the earliest due items first and respects limit", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		third := seed(t, s, u, base.Add(3*time.Minute))
		first := seed(t, s, u, base.Add(1*time.Minute))
		second := seed(t, s, u, base.Add(2*time.Minute))
		now := base.Add(time.Hour)

		CheckClaimed(t, claim(t, s, now, 2), []email.ClaimedItem{claimed(first, 1), claimed(second, 1)})
		CheckClaimed(t, claim(t, s, now, 2), []email.ClaimedItem{claimed(third, 1)})
		CheckClaimed(t, claim(t, s, now, 2), nil)
	})

	t.Run("MarkSent takes the item out of the queue", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base)
		claim(t, s, base, 10)

		if err := s.MarkSent(ctx, item.ID, base.Add(time.Minute)); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}

		CheckClaimed(t, claim(t, s, later, 10), nil)
	})

	t.Run("MarkFailed takes the item out of the queue", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base)
		claim(t, s, base, 10)

		if err := s.MarkFailed(ctx, item.ID, base.Add(time.Minute), "mailbox full"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		CheckClaimed(t, claim(t, s, later, 10), nil)
	})

	t.Run("Reschedule makes the item due again at the new time", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base)
		claim(t, s, base, 10)

		next := base.Add(time.Minute) // inside the old lease: Reschedule must release it
		if err := s.Reschedule(ctx, item.ID, next, "timeout"); err != nil {
			t.Fatalf("Reschedule: %v", err)
		}

		CheckClaimed(t, claim(t, s, next.Add(-tick), 10), nil)
		CheckClaimed(t, claim(t, s, next, 10), []email.ClaimedItem{claimed(item, 2)})
	})

	t.Run("a sent item cannot be rescheduled or failed", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base)
		claim(t, s, base, 10)
		if err := s.MarkSent(ctx, item.ID, base.Add(time.Minute)); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}

		if err := s.Reschedule(ctx, item.ID, base.Add(2*time.Minute), "late"); err != nil {
			t.Fatalf("Reschedule: %v", err)
		}
		if err := s.MarkFailed(ctx, item.ID, base.Add(2*time.Minute), "late"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		CheckClaimed(t, claim(t, s, later, 10), nil)
	})

	t.Run("Create rejects a duplicate id", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base)

		if err := s.Create(ctx, item); err == nil {
			t.Error("Create: err = nil, want an error")
		}
	})

	t.Run("DeleteSent removes items sent before the cutoff", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		sentAt := base.Add(time.Minute)
		item := seed(t, s, u, base)
		claim(t, s, base, 10)
		if err := s.MarkSent(ctx, item.ID, sentAt); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}

		tests := []struct {
			name   string
			before time.Time
			want   int64
		}{
			{"cutoff before the send", sentAt.Add(-time.Hour), 0},
			{"cutoff exactly at the send", sentAt, 0},
			{"cutoff after the send", sentAt.Add(tick), 1},
			{"already deleted", sentAt.Add(time.Hour), 0},
		}

		for _, tt := range tests {
			n, err := s.DeleteSent(ctx, tt.before, 100)
			if err != nil {
				t.Fatalf("%s: DeleteSent: %v", tt.name, err)
			}
			if n != tt.want {
				t.Errorf("%s: count = %d, want %d", tt.name, n, tt.want)
			}
		}
	})

	t.Run("DeleteSent only deletes sent items", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		sent := seed(t, s, u, base)
		failed := seed(t, s, u, base)
		pending := seed(t, s, u, base.Add(time.Hour))
		claim(t, s, base, 10) // leases sent and failed; pending is not due yet
		if err := s.MarkSent(ctx, sent.ID, base.Add(time.Minute)); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		if err := s.MarkFailed(ctx, failed.ID, base.Add(time.Minute), "boom"); err != nil {
			t.Fatalf("MarkFailed: %v", err)
		}

		n, err := s.DeleteSent(ctx, later, 100)
		if err != nil {
			t.Fatalf("DeleteSent: %v", err)
		}
		if n != 1 {
			t.Errorf("count = %d, want 1", n)
		}

		// The pending item is untouched.
		CheckClaimed(t, claim(t, s, later, 10), []email.ClaimedItem{claimed(pending, 1)})
	})

	t.Run("MarkSent keeps the first send time", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		item := seed(t, s, u, base)
		claim(t, s, base, 10)

		first := base.Add(time.Minute)
		if err := s.MarkSent(ctx, item.ID, first); err != nil {
			t.Fatalf("first MarkSent: %v", err)
		}
		if err := s.MarkSent(ctx, item.ID, first.Add(time.Hour)); err != nil {
			t.Fatalf("second MarkSent: %v", err)
		}

		n, err := s.DeleteSent(ctx, first.Add(tick), 100)
		if err != nil {
			t.Fatalf("DeleteSent: %v", err)
		}
		if n != 1 {
			t.Errorf("count = %d, want 1 (sent_at must stay at the first send)", n)
		}
	})

	t.Run("DeleteSent deletes at most limit items", func(t *testing.T) {
		s, us := newStore(t)
		u := authtest.SeedUser(ctx, t, us)
		for range 3 {
			item := seed(t, s, u, base)
			if err := s.MarkSent(ctx, item.ID, base.Add(time.Minute)); err != nil {
				t.Fatalf("MarkSent: %v", err)
			}
		}

		for _, want := range []int64{2, 1, 0} {
			n, err := s.DeleteSent(ctx, later, 2)
			if err != nil {
				t.Fatalf("DeleteSent: %v", err)
			}
			if n != want {
				t.Errorf("count = %d, want %d", n, want)
			}
		}
	})
}
