package postgres

import (
	"context"
	"errors"
	"testing"
	"uuid"

	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
)

// These tests cover the parts of the task store interfaces' contracts that the
// behaviour suites leave out: assignee handling, what Delete takes with it, and
// how closed or expired invitations are treated.

// newTaskAndShareDeps returns the share stores plus the task store of the same database.
func newTaskAndShareDeps(t *testing.T) (ShareDeps, task.Store, func() uuid.UUID) {
	t.Helper()
	d := newShareDeps(t, testutil.DatabaseURL(t))
	pool := newAuthPool(t, testutil.DatabaseURL(t))
	q := sqlcgen.New(pool)
	users := NewUserStore(q)
	newUser := func() uuid.UUID { return authtest.SeedUser(t.Context(), t, users).ID }
	return d, NewTaskStore(q), newUser
}

func assigneeOf(ctx context.Context, t *testing.T, s task.Store, owner, id uuid.UUID) *uuid.UUID {
	t.Helper()
	a, err := s.Get(ctx, owner, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return a.Task.AssigneeID
}

func TestAssigneeStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	pool := newAuthPool(t, url)
	q := sqlcgen.New(pool)
	users := NewUserStore(q)
	tasks := NewTaskStore(q)
	ctx := t.Context()

	newTask := func(t *testing.T) task.Task {
		t.Helper()
		owner := authtest.SeedUser(ctx, t, users)
		nt, err := task.New(task.NewParams{Title: "t", OwnerID: owner.ID, Status: task.StatusTodo}, shareBase)
		if err != nil {
			t.Fatalf("task.New: %v", err)
		}
		created, err := tasks.Create(ctx, nt)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		return created
	}
	newUser := func() uuid.UUID { return authtest.SeedUser(ctx, t, users).ID }

	t.Run("SetAssignee assigns and replaces the assignee", func(t *testing.T) {
		tk := newTask(t)
		first, second := newUser(), newUser()

		if err := tasks.SetAssignee(ctx, tk.ID, first); err != nil {
			t.Fatalf("SetAssignee: %v", err)
		}
		if got := assigneeOf(ctx, t, tasks, tk.OwnerID, tk.ID); got == nil || *got != first {
			t.Fatalf("assignee = %v, want %v", got, first)
		}
		if err := tasks.SetAssignee(ctx, tk.ID, second); err != nil {
			t.Fatalf("second SetAssignee: %v", err)
		}
		if got := assigneeOf(ctx, t, tasks, tk.OwnerID, tk.ID); got == nil || *got != second {
			t.Errorf("assignee = %v, want %v", got, second)
		}
	})

	t.Run("SetAssignee returns ErrNotFound for an unknown task", func(t *testing.T) {
		if err := tasks.SetAssignee(ctx, uuid.NewV7(), newUser()); !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("ClearAssignee clears whoever it is, and a task without one is fine", func(t *testing.T) {
		tk := newTask(t)
		if err := tasks.ClearAssignee(ctx, tk.ID); err != nil {
			t.Fatalf("ClearAssignee without an assignee: %v", err)
		}
		if err := tasks.SetAssignee(ctx, tk.ID, newUser()); err != nil {
			t.Fatalf("SetAssignee: %v", err)
		}
		if err := tasks.ClearAssignee(ctx, tk.ID); err != nil {
			t.Fatalf("ClearAssignee: %v", err)
		}
		if got := assigneeOf(ctx, t, tasks, tk.OwnerID, tk.ID); got != nil {
			t.Errorf("assignee = %v, want none", *got)
		}
	})

	t.Run("ClearAssignee returns ErrNotFound for an unknown task", func(t *testing.T) {
		if err := tasks.ClearAssignee(ctx, uuid.NewV7()); !errors.Is(err, task.ErrNotFound) {
			t.Errorf("err = %v, want %v", err, task.ErrNotFound)
		}
	})

	t.Run("ClearAssigneeIf clears only the named assignee", func(t *testing.T) {
		tk := newTask(t)
		assignee, other := newUser(), newUser()
		if err := tasks.SetAssignee(ctx, tk.ID, assignee); err != nil {
			t.Fatalf("SetAssignee: %v", err)
		}

		if err := tasks.ClearAssigneeIf(ctx, tk.ID, other); err != nil {
			t.Fatalf("ClearAssigneeIf with another user: %v", err)
		}
		if got := assigneeOf(ctx, t, tasks, tk.OwnerID, tk.ID); got == nil || *got != assignee {
			t.Fatalf("assignee = %v, want %v untouched", got, assignee)
		}
		if err := tasks.ClearAssigneeIf(ctx, tk.ID, assignee); err != nil {
			t.Fatalf("ClearAssigneeIf: %v", err)
		}
		if got := assigneeOf(ctx, t, tasks, tk.OwnerID, tk.ID); got != nil {
			t.Errorf("assignee = %v, want none", *got)
		}
	})

	t.Run("ClearAssigneeIf does nothing for a task without an assignee or an unknown task", func(t *testing.T) {
		tk := newTask(t)
		if err := tasks.ClearAssigneeIf(ctx, tk.ID, newUser()); err != nil {
			t.Errorf("task without assignee: %v", err)
		}
		if err := tasks.ClearAssigneeIf(ctx, uuid.NewV7(), newUser()); err != nil {
			t.Errorf("unknown task: %v", err)
		}
	})
}

func TestDeleteTaskTakesMembersAndInvitations(t *testing.T) {
	d, tasks, _ := newTaskAndShareDeps(t)
	ctx := t.Context()
	tk, _ := d.NewTask()
	userID, mail := d.NewUser()
	seedMember(ctx, t, d, tk.ID, userID, mail, task.RoleViewer)
	pending := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)

	if err := tasks.Delete(ctx, tk.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := d.Members.Get(ctx, tk.ID, userID); !errors.Is(err, task.ErrMemberNotFound) {
		t.Errorf("member after Delete: err = %v, want %v", err, task.ErrMemberNotFound)
	}
	if _, err := d.Invitations.GetByTokenHash(ctx, pending.TokenHash); !errors.Is(err, task.ErrInvitationNotFound) {
		t.Errorf("invitation after Delete: err = %v, want %v", err, task.ErrInvitationNotFound)
	}
}

func TestClosedAndExpiredInvitations(t *testing.T) {
	d, _, _ := newTaskAndShareDeps(t)
	ctx := t.Context()
	tk, _ := d.NewTask()

	for _, status := range []task.InvitationStatus{
		task.InvitationStatusAccepted, task.InvitationStatusDeclined, task.InvitationStatusCancelled,
	} {
		t.Run(string(status)+": GetPending and Cancel do not find it", func(t *testing.T) {
			inv := seedInvitation(ctx, t, d, tk.ID, status)
			if _, err := d.Invitations.GetPending(ctx, tk.ID, inv.Email, soon); !errors.Is(err, task.ErrInvitationNotFound) {
				t.Errorf("GetPending: err = %v, want %v", err, task.ErrInvitationNotFound)
			}
			if err := d.Invitations.Cancel(ctx, tk.ID, inv.ID, soon); !errors.Is(err, task.ErrInvitationNotFound) {
				t.Errorf("Cancel: err = %v, want %v", err, task.ErrInvitationNotFound)
			}
		})
	}

	t.Run("Cancel works just before the expiry and not at it", func(t *testing.T) {
		inv := seedInvitation(ctx, t, d, tk.ID, task.InvitationStatusPending)
		if err := d.Invitations.Cancel(ctx, tk.ID, inv.ID, inv.ExpiresAt); !errors.Is(err, task.ErrInvitationNotFound) {
			t.Fatalf("at the expiry: err = %v, want %v", err, task.ErrInvitationNotFound)
		}
		if err := d.Invitations.Cancel(ctx, tk.ID, inv.ID, inv.ExpiresAt.Add(-1)); err != nil {
			t.Errorf("just before the expiry: %v", err)
		}
	})

	t.Run("an expired pending invitation still blocks Create until it is removed", func(t *testing.T) {
		old := seedInvitationAt(ctx, t, d, tk.ID, task.InvitationStatusPending, shareBase)
		fresh := buildInvitationAt(t, tk.ID, old.Email, task.InvitationStatusPending, later)

		if err := d.Invitations.Create(ctx, fresh); !errors.Is(err, task.ErrInvitationExists) {
			t.Fatalf("Create over the expired one: err = %v, want %v", err, task.ErrInvitationExists)
		}
		if _, err := d.Invitations.DeleteExpiredPendingFor(ctx, tk.ID, old.Email, later); err != nil {
			t.Fatalf("DeleteExpiredPendingFor: %v", err)
		}
		if err := d.Invitations.Create(ctx, fresh); err != nil {
			t.Errorf("Create after removing the expired one: %v", err)
		}
	})
}
