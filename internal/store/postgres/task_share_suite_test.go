package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

// ShareDeps is what the [task.InvitationStore] and [task.MemberStore] suites
// need. Tasks and members belong to users, and a real database enforces that
// with foreign keys, so the suites cannot make those rows themselves.
type ShareDeps struct {
	Invitations task.InvitationStore
	Members     task.MemberStore

	// NewTask persists a task owned by a fresh user and returns it with the
	// owner's email.
	NewTask func() (tk task.Task, ownerEmail string)

	// NewUser persists a fresh user.
	NewUser func() (id uuid.UUID, email string)
}

var shareBase = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// invitationLifetime is how long an invitation lives (task.NewInvitation fixes
// it): one created at shareBase has expired at shareBase+invitationLifetime.
const invitationLifetime = 30 * 24 * time.Hour

// Times the suites call the stores with: soon after shareBase (nothing has
// expired yet) and long after it (every invitation has).
var (
	soon  = shareBase.Add(time.Minute)
	later = shareBase.Add(invitationLifetime + time.Hour)
)

func hashFor() []byte {
	sum := sha256.Sum256([]byte(uuid.NewV7().String()))
	return sum[:]
}

func newEmail() string {
	return fmt.Sprintf("invitee-%s@example.com", uuid.NewV7())
}

// buildInvitation returns a viewer invitation created at shareBase that is not
// persisted.
func buildInvitation(t *testing.T, taskID uuid.UUID, email string, status task.InvitationStatus) task.Invitation {
	t.Helper()
	return buildInvitationAt(t, taskID, email, status, shareBase)
}

// buildInvitationAt is like buildInvitation for an invitation created at createdAt.
func buildInvitationAt(t *testing.T, taskID uuid.UUID, email string, status task.InvitationStatus, createdAt time.Time) task.Invitation {
	t.Helper()

	inv, err := task.NewInvitation(task.NewInvitationParams{
		TaskID: taskID, Email: email, Role: task.RoleViewer,
	}, createdAt)
	if err != nil {
		t.Fatalf("task.NewInvitation: %v", err)
	}

	// NewInvitation always returns a pending invitation without a token hash,
	// so the suites set what they need directly.
	inv.Status = status
	inv.TokenHash = hashFor()

	return inv
}

// seedInvitation persists an invitation to a task with a fresh email.
func seedInvitation(ctx context.Context, t *testing.T, d ShareDeps, taskID uuid.UUID, status task.InvitationStatus) task.Invitation {
	t.Helper()
	return seedInvitationFor(ctx, t, d, taskID, newEmail(), status)
}

// seedInvitationAt persists an invitation created at createdAt, so a test can
// make one that has already expired.
func seedInvitationAt(ctx context.Context, t *testing.T, d ShareDeps, taskID uuid.UUID, status task.InvitationStatus, createdAt time.Time) task.Invitation {
	t.Helper()

	inv := buildInvitationAt(t, taskID, newEmail(), status, createdAt)
	if err := d.Invitations.Create(ctx, inv); err != nil {
		t.Fatalf("Create invitation: %v", err)
	}

	return inv
}

func seedInvitationFor(ctx context.Context, t *testing.T, d ShareDeps, taskID uuid.UUID, email string, status task.InvitationStatus) task.Invitation {
	t.Helper()

	inv := buildInvitation(t, taskID, email, status)
	if err := d.Invitations.Create(ctx, inv); err != nil {
		t.Fatalf("Create invitation: %v", err)
	}

	return inv
}

// seedMember persists a member of the task. A member comes from an accepted
// invitation, so it creates that first.
func seedMember(ctx context.Context, t *testing.T, d ShareDeps, taskID, userID uuid.UUID, email string, role task.Role) task.Member {
	t.Helper()

	inv := seedInvitationFor(ctx, t, d, taskID, email, task.InvitationStatusAccepted)
	m, err := task.NewMember(task.NewMemberParams{
		TaskID: taskID, UserID: userID, Role: role, InvitationID: inv.ID,
	}, shareBase)
	if err != nil {
		t.Fatalf("task.NewMember: %v", err)
	}
	if err := d.Members.Create(ctx, m); err != nil {
		t.Fatalf("Create member: %v", err)
	}

	return m
}

// TestInvitationStore runs the behaviour suite that every [task.InvitationStore]
// must pass. newDeps must return empty stores.
func runInvitationStoreSuite(t *testing.T, newDeps func(t *testing.T) ShareDeps) {
	t.Helper()
	ctx := t.Context()

	// missing checks that the invitation was removed.
	missing := func(t *testing.T, d ShareDeps, inv task.Invitation) {
		t.Helper()

		_, err := d.Invitations.GetByTokenHash(ctx, inv.TokenHash)
		if !errors.Is(err, task.ErrInvitationNotFound) {
			t.Errorf("GetByTokenHash: err = %v, want %v", err, task.ErrInvitationNotFound)
		}
	}

	// present checks that the invitation is still stored, unchanged.
	present := func(t *testing.T, d ShareDeps, inv task.Invitation) {
		t.Helper()

		got, err := d.Invitations.GetByTokenHash(ctx, inv.TokenHash)
		if err != nil {
			t.Fatalf("GetByTokenHash: %v", err)
		}
		tasktest.CheckInvitation(t, got, inv)
	}

	t.Run("create then get by token hash", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()

		inv := buildInvitation(t, tk.ID, newEmail(), task.InvitationStatusPending)
		inv.AssignOnAccept = true
		if err := d.Invitations.Create(ctx, inv); err != nil {
			t.Fatalf("Create: %v", err)
		}

		present(t, d, inv)
	})

	t.Run("Create returns ErrInvitationExists for a second pending invitation", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		first := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		dup := buildInvitation(t, tk.ID, first.Email, task.InvitationStatusPending)
		err := d.Invitations.Create(ctx, dup)
		if !errors.Is(err, task.ErrInvitationExists) {
			t.Errorf("err = %v, want %v", err, task.ErrInvitationExists)
		}
	})

	t.Run("Create allows a new invitation once the old one is closed", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		first := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusDeclined)

		seedInvitationFor(ctx, t, d, tk.ID, first.Email, task.InvitationStatusPending)
	})

	t.Run("Create allows the same email on another task", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		other, _ := d.NewTask()
		first := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		seedInvitationFor(ctx, t, d, other.ID, first.Email, task.InvitationStatusPending)
	})

	t.Run("Create returns ErrNotFound for unknown task", func(t *testing.T) {
		d := newDeps(t)

		inv := buildInvitation(t, uuid.NewV7(), newEmail(), task.InvitationStatusPending)
		err := d.Invitations.Create(ctx, inv)
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("GetByTokenHash returns ErrInvitationNotFound for unknown hash", func(t *testing.T) {
		d := newDeps(t)

		_, err := d.Invitations.GetByTokenHash(ctx, hashFor())
		if !errors.Is(err, task.ErrInvitationNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrInvitationNotFound)
		}
	})

	t.Run("GetForMail returns the invitation with its task title and owner email", func(t *testing.T) {
		d := newDeps(t)
		tk, ownerEmail := d.NewTask()
		inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		got, err := d.Invitations.GetForMail(ctx, inv.ID)
		if err != nil {
			t.Fatalf("GetForMail: %v", err)
		}

		tasktest.CheckInvitation(t, got.Invitation, inv)
		if got.TaskTitle != tk.Title {
			t.Errorf("TaskTitle = %q, want %q", got.TaskTitle, tk.Title)
		}
		if got.OwnerEmail != ownerEmail {
			t.Errorf("OwnerEmail = %q, want %q", got.OwnerEmail, ownerEmail)
		}
	})

	t.Run("GetForMail returns ErrInvitationNotFound for unknown id", func(t *testing.T) {
		d := newDeps(t)

		_, err := d.Invitations.GetForMail(ctx, uuid.NewV7())
		if !errors.Is(err, task.ErrInvitationNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrInvitationNotFound)
		}
	})

	t.Run("SetStatus updates status and update time", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		if err := d.Invitations.SetStatus(ctx, inv.ID, task.InvitationStatusAccepted, soon); err != nil {
			t.Fatalf("SetStatus: %v", err)
		}

		want := inv
		want.Status = task.InvitationStatusAccepted
		want.UpdatedAt = soon
		present(t, d, want)
	})

	t.Run("SetStatus returns ErrInvitationNotFound for unknown id", func(t *testing.T) {
		d := newDeps(t)

		err := d.Invitations.SetStatus(ctx, uuid.NewV7(), task.InvitationStatusAccepted, soon)
		if !errors.Is(err, task.ErrInvitationNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrInvitationNotFound)
		}
	})

	t.Run("SetTokenHash replaces the hash", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		newHash := hashFor()
		if err := d.Invitations.SetTokenHash(ctx, inv.ID, newHash, soon); err != nil {
			t.Fatalf("SetTokenHash: %v", err)
		}

		missing(t, d, inv)

		want := inv
		want.TokenHash = newHash
		want.UpdatedAt = soon
		present(t, d, want)
	})

	t.Run("SetTokenHash returns ErrInvitationNotFound for unknown id", func(t *testing.T) {
		d := newDeps(t)

		err := d.Invitations.SetTokenHash(ctx, uuid.NewV7(), hashFor(), soon)
		if !errors.Is(err, task.ErrInvitationNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrInvitationNotFound)
		}
	})

	t.Run("DeleteExpiredPendingFor removes only the expired pending invitation", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		other, _ := d.NewTask()
		email := newEmail()

		accepted := seedInvitationFor(ctx, t, d, tk.ID, email, task.InvitationStatusAccepted)
		stale := seedInvitationFor(ctx, t, d, tk.ID, email, task.InvitationStatusPending)
		elsewhere := seedInvitationFor(ctx, t, d, other.ID, email, task.InvitationStatusPending)
		neighbour := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		deleted, err := d.Invitations.DeleteExpiredPendingFor(ctx, tk.ID, email, later)
		if err != nil {
			t.Fatalf("DeleteExpiredPendingFor: %v", err)
		}
		if deleted != 1 {
			t.Errorf("deleted = %d, want 1", deleted)
		}

		missing(t, d, stale)
		present(t, d, accepted)
		present(t, d, elsewhere)
		present(t, d, neighbour)

		// The email can be invited again, and deleting nothing is not an error.
		seedInvitationFor(ctx, t, d, tk.ID, email, task.InvitationStatusPending)
		deleted, err = d.Invitations.DeleteExpiredPendingFor(ctx, tk.ID, newEmail(), later)
		if err != nil {
			t.Errorf("DeleteExpiredPendingFor with nothing to delete: %v", err)
		}
		if deleted != 0 {
			t.Errorf("deleted with nothing to delete = %d, want 0", deleted)
		}
	})

	t.Run("DeleteExpiredPendingFor keeps a pending invitation until it expires", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		deleted, err := d.Invitations.DeleteExpiredPendingFor(ctx, tk.ID, inv.Email, inv.ExpiresAt.Add(-time.Nanosecond))
		if err != nil {
			t.Fatalf("DeleteExpiredPendingFor: %v", err)
		}
		if deleted != 0 {
			t.Errorf("deleted before expiry = %d, want 0", deleted)
		}
		present(t, d, inv)

		// It is deleted at the moment it expires.
		deleted, err = d.Invitations.DeleteExpiredPendingFor(ctx, tk.ID, inv.Email, inv.ExpiresAt)
		if err != nil {
			t.Fatalf("DeleteExpiredPendingFor at expiry: %v", err)
		}
		if deleted != 1 {
			t.Errorf("deleted at expiry = %d, want 1", deleted)
		}
		missing(t, d, inv)
	})

	t.Run("CountPendingByOwner counts only pending invitations of the owner's tasks", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		other, _ := d.NewTask()

		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)
		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)
		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusAccepted)
		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusDeclined)
		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusCancelled)
		seedInvitation(ctx, t, d, other.ID, task.InvitationStatusPending)

		tests := []struct {
			name  string
			owner uuid.UUID
			want  int64
		}{
			{"owner with several invitations", tk.OwnerID, 2},
			{"another owner", other.OwnerID, 1},
			{"owner of nothing", uuid.NewV7(), 0},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := d.Invitations.CountPendingByOwner(ctx, tt.owner)
				if err != nil {
					t.Fatalf("CountPendingByOwner: %v", err)
				}
				if got != tt.want {
					t.Errorf("count = %d, want %d", got, tt.want)
				}
			})
		}
	})

	t.Run("DeleteExpiredPending removes up to limit expired pending invitations", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()

		expired := []task.Invitation{
			seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending),
			seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending),
			seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending),
		}
		fresh := seedInvitationAt(ctx, t, d, tk.ID, task.InvitationStatusPending, later)
		accepted := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusAccepted)
		declined := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusDeclined)

		tests := []struct {
			name  string
			limit int32
			want  int64
		}{
			{"limit smaller than the matches", 2, 2},
			{"limit larger than the matches", 10, 1},
			{"nothing left", 10, 0},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := d.Invitations.DeleteExpiredPending(ctx, later, tt.limit)
				if err != nil {
					t.Fatalf("DeleteExpiredPending: %v", err)
				}
				if got != tt.want {
					t.Errorf("deleted = %d, want %d", got, tt.want)
				}
			})
		}

		for _, inv := range expired {
			missing(t, d, inv)
		}
		present(t, d, fresh)
		present(t, d, accepted)
		present(t, d, declined)
	})

	t.Run("DeleteClosed removes up to limit expired declined or cancelled invitations", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()

		closed := []task.Invitation{
			seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusDeclined),
			seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusCancelled),
			seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusDeclined),
		}
		fresh := seedInvitationAt(ctx, t, d, tk.ID, task.InvitationStatusDeclined, later)
		pending := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)
		accepted := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusAccepted)

		tests := []struct {
			name  string
			limit int32
			want  int64
		}{
			{"limit smaller than the matches", 2, 2},
			{"limit larger than the matches", 10, 1},
			{"nothing left", 10, 0},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := d.Invitations.DeleteClosed(ctx, later, tt.limit)
				if err != nil {
					t.Fatalf("DeleteClosed: %v", err)
				}
				if got != tt.want {
					t.Errorf("deleted = %d, want %d", got, tt.want)
				}
			})
		}

		for _, inv := range closed {
			missing(t, d, inv)
		}
		present(t, d, fresh)
		present(t, d, pending)
		present(t, d, accepted)
	})
}

// TestMemberStore runs the behaviour suite that every [task.MemberStore] must
// pass. newDeps must return empty stores.
func runMemberStoreSuite(t *testing.T, newDeps func(t *testing.T) ShareDeps) {
	t.Helper()
	ctx := t.Context()

	t.Run("create then get again", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		userID, email := d.NewUser()
		m := seedMember(ctx, t, d, tk.ID, userID, email, task.RoleEditor)

		got, err := d.Members.Get(ctx, tk.ID, userID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		tasktest.CheckMember(t, got, m)
	})

	t.Run("Create returns ErrMemberExists for an existing member", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		userID, email := d.NewUser()
		seedMember(ctx, t, d, tk.ID, userID, email, task.RoleViewer)

		inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusAccepted)
		dup, err := task.NewMember(task.NewMemberParams{
			TaskID: tk.ID, UserID: userID, Role: task.RoleManager, InvitationID: inv.ID,
		}, shareBase)
		if err != nil {
			t.Fatalf("task.NewMember: %v", err)
		}

		err = d.Members.Create(ctx, dup)
		if !errors.Is(err, task.ErrMemberExists) {
			t.Errorf("err = %v, want %v", err, task.ErrMemberExists)
		}
	})

	t.Run("Create returns ErrNotFound for unknown task", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		userID, _ := d.NewUser()

		inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusAccepted)
		m, err := task.NewMember(task.NewMemberParams{
			TaskID: uuid.NewV7(), UserID: userID, Role: task.RoleViewer, InvitationID: inv.ID,
		}, shareBase)
		if err != nil {
			t.Fatalf("task.NewMember: %v", err)
		}

		err = d.Members.Create(ctx, m)
		if !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("Get returns ErrMemberNotFound for a user who is not a member", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		userID, _ := d.NewUser()

		_, err := d.Members.Get(ctx, tk.ID, userID)
		if !errors.Is(err, task.ErrMemberNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrMemberNotFound)
		}
	})

	t.Run("Get does not find a member of another task", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		other, _ := d.NewTask()
		userID, email := d.NewUser()
		seedMember(ctx, t, d, tk.ID, userID, email, task.RoleViewer)

		_, err := d.Members.Get(ctx, other.ID, userID)
		if !errors.Is(err, task.ErrMemberNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrMemberNotFound)
		}
	})

	t.Run("SetRole updates the role and returns the share", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		userID, email := d.NewUser()
		m := seedMember(ctx, t, d, tk.ID, userID, email, task.RoleViewer)

		got, err := d.Members.SetRole(ctx, tk.ID, userID, task.RoleManager, soon)
		if err != nil {
			t.Fatalf("SetRole: %v", err)
		}

		tasktest.CheckShare(t, got, task.Share{
			Kind: task.ShareKindMember, ID: userID, Email: email,
			Role: task.RoleManager, CreatedAt: m.CreatedAt,
		})

		want := m
		want.Role = task.RoleManager
		want.UpdatedAt = soon
		stored, err := d.Members.Get(ctx, tk.ID, userID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		tasktest.CheckMember(t, stored, want)
	})

	t.Run("SetRole returns ErrMemberNotFound for a user who is not a member", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		userID, _ := d.NewUser()

		_, err := d.Members.SetRole(ctx, tk.ID, userID, task.RoleEditor, soon)
		if !errors.Is(err, task.ErrMemberNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrMemberNotFound)
		}
	})

	t.Run("Remove deletes the member", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		userID, email := d.NewUser()
		seedMember(ctx, t, d, tk.ID, userID, email, task.RoleViewer)

		if err := d.Members.Remove(ctx, tk.ID, userID); err != nil {
			t.Fatalf("Remove: %v", err)
		}

		_, err := d.Members.Get(ctx, tk.ID, userID)
		if !errors.Is(err, task.ErrMemberNotFound) {
			t.Errorf("Get after Remove: err = %v, want %v", err, task.ErrMemberNotFound)
		}

		err = d.Members.Remove(ctx, tk.ID, userID)
		if !errors.Is(err, task.ErrMemberNotFound) {
			t.Errorf("second Remove: err = %v, want %v", err, task.ErrMemberNotFound)
		}
	})

	t.Run("Count counts the members of the task only", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		other, _ := d.NewTask()

		for range 2 {
			userID, email := d.NewUser()
			seedMember(ctx, t, d, tk.ID, userID, email, task.RoleViewer)
		}
		userID, email := d.NewUser()
		seedMember(ctx, t, d, other.ID, userID, email, task.RoleViewer)
		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		tests := []struct {
			name   string
			taskID uuid.UUID
			want   int64
		}{
			{"task with members and a pending invitation", tk.ID, 2},
			{"another task", other.ID, 1},
			{"unknown task", uuid.NewV7(), 0},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := d.Members.Count(ctx, tt.taskID)
				if err != nil {
					t.Fatalf("Count: %v", err)
				}
				if got != tt.want {
					t.Errorf("count = %d, want %d", got, tt.want)
				}
			})
		}
	})

	t.Run("ListShares returns members and unexpired pending invitations", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()
		other, _ := d.NewTask()

		userID, email := d.NewUser()
		m := seedMember(ctx, t, d, tk.ID, userID, email, task.RoleEditor)
		pending := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

		// None of these belong in the list. The first one was created so long ago
		// that it expired before now.
		seedInvitationAt(ctx, t, d, tk.ID, task.InvitationStatusPending, shareBase.Add(-invitationLifetime-time.Hour))
		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusDeclined)
		seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusCancelled)
		otherUser, otherEmail := d.NewUser()
		seedMember(ctx, t, d, other.ID, otherUser, otherEmail, task.RoleViewer)

		now := shareBase.Add(2 * time.Hour)
		got, err := d.Members.ListShares(ctx, tk.ID, now)
		if err != nil {
			t.Fatalf("ListShares: %v", err)
		}

		want := []task.Share{
			{
				Kind: task.ShareKindMember, ID: userID, Email: email,
				Role: task.RoleEditor, CreatedAt: m.CreatedAt,
			},
			{
				Kind: task.ShareKindInvitation, ID: pending.ID, Email: pending.Email,
				Role: pending.Role, CreatedAt: pending.CreatedAt, ExpiresAt: &pending.ExpiresAt,
			},
		}

		// The order is unspecified, so match the shares up by ID.
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d: %+v", len(got), len(want), got)
		}
		for _, sh := range got {
			i := slices.IndexFunc(want, func(w task.Share) bool { return w.ID == sh.ID })
			if i < 0 {
				t.Errorf("unexpected share %+v", sh)
				continue
			}
			tasktest.CheckShare(t, sh, want[i])
		}
	})

	t.Run("ListShares returns an empty, non-nil slice when nothing is shared", func(t *testing.T) {
		d := newDeps(t)
		tk, _ := d.NewTask()

		got, err := d.Members.ListShares(ctx, tk.ID, soon)
		if err != nil {
			t.Fatalf("ListShares: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("shares = %#v, want an empty, non-nil slice", got)
		}
	})
}
