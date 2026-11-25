package postgres

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
)

// These tests cover the SQL of the sharing stores that the behaviour suites do
// not pin: time boundaries, filtering by task, ordering and who counts as a
// verified user. They run sequentially against this package's database.

func TestGetVerifiedUserIDByEmail(t *testing.T) {
	url := testutil.DatabaseURL(t)
	pool := newAuthPool(t, url)
	users := NewUserStore(sqlcgen.New(pool))
	ctx := t.Context()

	verified := authtest.SeedUser(ctx, t, users)
	if err := users.SetEmailVerified(ctx, verified.ID, time.Now()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	unverified := authtest.SeedUser(ctx, t, users)
	disabled := authtest.SeedUser(ctx, t, users)
	if err := users.SetEmailVerified(ctx, disabled.ID, time.Now()); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET disabled_at = now() WHERE id = $1`, disabled.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}

	tests := []struct {
		name  string
		email string
		want  uuid.UUID
		err   error
	}{
		{"verified", verified.Email, verified.ID, nil},
		{"unverified", unverified.Email, uuid.Nil(), task.ErrUserNotFound},
		{"disabled", disabled.Email, uuid.Nil(), task.ErrUserNotFound},
		{"unknown", "nobody@example.com", uuid.Nil(), task.ErrUserNotFound},
		// Callers pass an already normalized (lower case) address; the store
		// does not guess, so another spelling is not found.
		{"upper case is not normalized here", upperASCII(verified.Email), uuid.Nil(), task.ErrUserNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := users.GetVerifiedUserIDByEmail(ctx, tt.email)
			if !errors.Is(err, tt.err) {
				t.Fatalf("error = %v, want %v", err, tt.err)
			}
			if got != tt.want {
				t.Errorf("id = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInvitationTimeBoundaries(t *testing.T) {
	url := testutil.DatabaseURL(t)
	d := newShareDeps(t, url)
	ctx := t.Context()
	tk, _ := d.NewTask()
	inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)
	expiry := inv.ExpiresAt

	t.Run("GetPending: found just before the expiry, not at it", func(t *testing.T) {
		if _, err := d.Invitations.GetPending(ctx, tk.ID, inv.Email, expiry.Add(-time.Microsecond)); err != nil {
			t.Errorf("just before: %v", err)
		}
		if _, err := d.Invitations.GetPending(ctx, tk.ID, inv.Email, expiry); !errors.Is(err, task.ErrInvitationNotFound) {
			t.Errorf("at the expiry: error = %v, want ErrInvitationNotFound", err)
		}
	})

	t.Run("DeleteExpiredPending keeps it just before the expiry and deletes it at it", func(t *testing.T) {
		if n, err := d.Invitations.DeleteExpiredPending(ctx, expiry.Add(-time.Microsecond), 10); err != nil || n != 0 {
			t.Fatalf("just before: (%d, %v), want 0", n, err)
		}
		if n, err := d.Invitations.DeleteExpiredPending(ctx, expiry, 10); err != nil || n != 1 {
			t.Fatalf("at the expiry: (%d, %v), want 1", n, err)
		}
	})

	t.Run("DeleteClosed keeps a closed invitation just before the expiry and deletes it at it", func(t *testing.T) {
		closed := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusDeclined)
		if n, err := d.Invitations.DeleteClosed(ctx, closed.ExpiresAt.Add(-time.Microsecond), 10); err != nil || n != 0 {
			t.Fatalf("just before: (%d, %v), want 0", n, err)
		}
		if n, err := d.Invitations.DeleteClosed(ctx, closed.ExpiresAt, 10); err != nil || n != 1 {
			t.Fatalf("at the expiry: (%d, %v), want 1", n, err)
		}
	})
}

func TestDeleteClosedKeepsAcceptedInvitationOfAMember(t *testing.T) {
	url := testutil.DatabaseURL(t)
	d := newShareDeps(t, url)
	ctx := t.Context()
	tk, _ := d.NewTask()
	userID, email := d.NewUser()
	m := seedMember(ctx, t, d, tk.ID, userID, email, task.RoleViewer)

	if _, err := d.Invitations.DeleteClosed(ctx, later, 100); err != nil {
		t.Fatalf("DeleteClosed: %v", err)
	}
	if _, err := d.Members.Get(ctx, tk.ID, userID); err != nil {
		t.Fatalf("the member is gone: %v", err)
	}
	var n int
	if err := rawPool(t, url).QueryRow(ctx, `SELECT count(*) FROM task_invitations WHERE id = $1`, m.InvitationID).Scan(&n); err != nil || n != 1 {
		t.Errorf("accepted invitation rows = %d (err %v), want 1: a member still references it", n, err)
	}
	// The same holds for DeleteExpiredPending: it only takes pending rows.
	if _, err := d.Invitations.DeleteExpiredPending(ctx, later, 100); err != nil {
		t.Fatalf("DeleteExpiredPending: %v", err)
	}
	if _, err := d.Members.Get(ctx, tk.ID, userID); err != nil {
		t.Fatalf("the member is gone after DeleteExpiredPending: %v", err)
	}
}

func TestCancelFiltersByTask(t *testing.T) {
	url := testutil.DatabaseURL(t)
	d := newShareDeps(t, url)
	ctx := t.Context()
	mine, _ := d.NewTask()
	other, _ := d.NewTask()
	inv := seedInvitation(ctx, t, d, other.ID, task.InvitationStatusPending)

	if err := d.Invitations.Cancel(ctx, mine.ID, inv.ID, soon); !errors.Is(err, task.ErrInvitationNotFound) {
		t.Fatalf("Cancel with another task's id = %v, want ErrInvitationNotFound", err)
	}
	if _, err := d.Invitations.GetPending(ctx, other.ID, inv.Email, soon); err != nil {
		t.Errorf("the invitation must stay pending: %v", err)
	}
	if err := d.Invitations.Cancel(ctx, other.ID, inv.ID, soon); err != nil {
		t.Errorf("Cancel with the right task: %v", err)
	}
	if err := d.Invitations.Cancel(ctx, other.ID, inv.ID, soon); !errors.Is(err, task.ErrInvitationNotFound) {
		t.Errorf("Cancel of an already cancelled invitation = %v, want ErrInvitationNotFound", err)
	}
}

func TestListSharesOrderingAndFiltering(t *testing.T) {
	url := testutil.DatabaseURL(t)
	d := newShareDeps(t, url)
	ctx := t.Context()
	tk, _ := d.NewTask()
	other, _ := d.NewTask()
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	now := base.Add(time.Hour)

	// Created out of order, with a tie on created_at between an invitation and a member.
	late := seedInvitationAt(ctx, t, d, tk.ID, task.InvitationStatusPending, base.Add(30*time.Minute))
	userID, email := d.NewUser()
	inv := seedInvitationAt(ctx, t, d, tk.ID, task.InvitationStatusAccepted, base)
	member, err := task.NewMember(task.NewMemberParams{TaskID: tk.ID, UserID: userID, Role: task.RoleViewer, InvitationID: inv.ID}, base.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("NewMember: %v", err)
	}
	if err := d.Members.Create(ctx, member); err != nil {
		t.Fatalf("Create member: %v", err)
	}
	_ = email
	tie := seedInvitationAt(ctx, t, d, tk.ID, task.InvitationStatusPending, base.Add(10*time.Minute))
	seedInvitationAt(ctx, t, d, other.ID, task.InvitationStatusPending, base) // another task: never listed

	shares, err := d.Members.ListShares(ctx, tk.ID, now)
	if err != nil {
		t.Fatalf("ListShares: %v", err)
	}
	if len(shares) != 3 {
		t.Fatalf("shares = %d, want 3 (the member and two pending invitations)", len(shares))
	}
	for i := 1; i < len(shares); i++ {
		a, b := shares[i-1], shares[i]
		if b.CreatedAt.Before(a.CreatedAt) || (b.CreatedAt.Equal(a.CreatedAt) && b.ID.String() < a.ID.String()) {
			t.Errorf("shares %d and %d are out of order by (created_at, id): %+v then %+v", i-1, i, a, b)
		}
	}
	if last := shares[2]; last.ID != late.ID {
		t.Errorf("the last share = %v, want the latest invitation %v", last.ID, late.ID)
	}
	if shares[0].CreatedAt.Equal(shares[1].CreatedAt) == false {
		t.Errorf("the first two shares should be the tie (member %v, invitation %v)", userID, tie.ID)
	}
}

func TestListSharesExpiryBoundary(t *testing.T) {
	url := testutil.DatabaseURL(t)
	d := newShareDeps(t, url)
	ctx := t.Context()
	tk, _ := d.NewTask()
	inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

	before, err := d.Members.ListShares(ctx, tk.ID, inv.ExpiresAt.Add(-time.Microsecond))
	if err != nil || len(before) != 1 {
		t.Fatalf("just before the expiry: (%v, %v), want the invitation", before, err)
	}
	at, err := d.Members.ListShares(ctx, tk.ID, inv.ExpiresAt)
	if err != nil || len(at) != 0 {
		t.Fatalf("at the expiry: (%v, %v), want nothing", at, err)
	}
}

func upperASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// rawPool connects without clearing any table, for checks next to a suite's own stores.
func rawPool(t *testing.T, url string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
