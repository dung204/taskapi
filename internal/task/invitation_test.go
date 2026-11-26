package task_test

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/task"
)

func TestNewInvitation(t *testing.T) {
	t.Parallel()
	// A time with nanoseconds and a non-UTC zone: the invitation keeps UTC and microseconds.
	now := time.Date(2026, 5, 1, 19, 30, 15, 123456789, time.FixedZone("x", 7*3600))
	taskID := uuid.NewV7()

	tests := []struct {
		name      string
		email     string
		role      task.Role
		wantEmail string
		wantErr   error
	}{
		{name: "plain", email: "an@example.com", role: task.RoleViewer, wantEmail: "an@example.com"},
		{name: "upper case is lowered", email: "An@Example.COM", role: task.RoleEditor, wantEmail: "an@example.com"},
		{name: "whitespace is trimmed", email: "  an@example.com\t", role: task.RoleManager, wantEmail: "an@example.com"},
		{name: "contributor", email: "an@example.com", role: task.RoleContributor, wantEmail: "an@example.com"},
		{name: "empty email", email: "", role: task.RoleViewer, wantErr: email.ErrInvalidEmail},
		{name: "not an email", email: "an.example.com", role: task.RoleViewer, wantErr: email.ErrInvalidEmail},
		{name: "the owner role cannot be granted", email: "an@example.com", role: task.RoleOwner, wantErr: task.ErrInvalidRole},
		{name: "unknown role", email: "an@example.com", role: "admin", wantErr: task.ErrInvalidRole},
		{name: "empty role", email: "an@example.com", role: "", wantErr: task.ErrInvalidRole},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, err := task.NewInvitation(task.NewInvitationParams{TaskID: taskID, Email: tt.email, Role: tt.role, AssignOnAccept: true}, now)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewInvitation: %v", err)
			}
			wantCreated := time.Date(2026, 5, 1, 12, 30, 15, 123456000, time.UTC)
			if inv.Email != tt.wantEmail || inv.Role != tt.role || inv.TaskID != taskID || !inv.AssignOnAccept {
				t.Errorf("invitation = %+v", inv)
			}
			if inv.Status != task.InvitationStatusPending {
				t.Errorf("status = %q, want pending", inv.Status)
			}
			if !inv.CreatedAt.Equal(wantCreated) || inv.CreatedAt.Location() != time.UTC || !inv.UpdatedAt.Equal(wantCreated) {
				t.Errorf("created/updated = %v/%v, want %v in UTC", inv.CreatedAt, inv.UpdatedAt, wantCreated)
			}
			if want := wantCreated.Add(30 * 24 * time.Hour); !inv.ExpiresAt.Equal(want) {
				t.Errorf("expires_at = %v, want %v (30 days)", inv.ExpiresAt, want)
			}
			if len(inv.TokenHash) != 0 {
				t.Errorf("a new invitation has no token yet")
			}
		})
	}

	t.Run("ids are unique", func(t *testing.T) {
		a, _ := task.NewInvitation(task.NewInvitationParams{TaskID: taskID, Email: "a@example.com", Role: task.RoleViewer}, now)
		b, _ := task.NewInvitation(task.NewInvitationParams{TaskID: taskID, Email: "a@example.com", Role: task.RoleViewer}, now)
		if a.ID == b.ID {
			t.Errorf("two invitations share the id %v", a.ID)
		}
	})
}

func TestInvitationIsPending(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	inv, err := task.NewInvitation(task.NewInvitationParams{TaskID: uuid.NewV7(), Email: "a@example.com", Role: task.RoleViewer}, now)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		status task.InvitationStatus
		at     time.Time
		want   bool
	}{
		{"just created", task.InvitationStatusPending, now, true},
		{"one microsecond before the expiry", task.InvitationStatusPending, inv.ExpiresAt.Add(-time.Microsecond), true},
		{"exactly at the expiry", task.InvitationStatusPending, inv.ExpiresAt, false},
		{"after the expiry", task.InvitationStatusPending, inv.ExpiresAt.Add(time.Second), false},
		{"accepted", task.InvitationStatusAccepted, now, false},
		{"declined", task.InvitationStatusDeclined, now, false},
		{"cancelled", task.InvitationStatusCancelled, now, false},
		{"unknown status", "weird", now, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := inv
			i.Status = tt.status
			if got := i.IsPending(tt.at); got != tt.want {
				t.Errorf("IsPending(%v) = %t, want %t", tt.at, got, tt.want)
			}
		})
	}
}

func TestNewMember(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 1, 19, 30, 15, 123456789, time.FixedZone("x", 7*3600))
	p := task.NewMemberParams{TaskID: uuid.NewV7(), UserID: uuid.NewV7(), InvitationID: uuid.NewV7()}

	for _, role := range []task.Role{task.RoleViewer, task.RoleContributor, task.RoleEditor, task.RoleManager} {
		t.Run(string(role), func(t *testing.T) {
			p.Role = role
			m, err := task.NewMember(p, now)
			if err != nil {
				t.Fatalf("NewMember: %v", err)
			}
			want := time.Date(2026, 5, 1, 12, 30, 15, 123456000, time.UTC)
			if m.TaskID != p.TaskID || m.UserID != p.UserID || m.InvitationID != p.InvitationID || m.Role != role {
				t.Errorf("member = %+v, want the params back", m)
			}
			if !m.CreatedAt.Equal(want) || !m.UpdatedAt.Equal(want) || m.CreatedAt.Location() != time.UTC {
				t.Errorf("created/updated = %v/%v, want %v in UTC", m.CreatedAt, m.UpdatedAt, want)
			}
		})
	}

	for _, role := range []task.Role{task.RoleOwner, "admin", ""} {
		t.Run("role "+string(role)+" is refused", func(t *testing.T) {
			p.Role = role
			if _, err := task.NewMember(p, now); !errors.Is(err, task.ErrInvalidRole) {
				t.Errorf("error = %v, want ErrInvalidRole", err)
			}
		})
	}
}
