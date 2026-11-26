package task_test

import (
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/opaquetoken"
	"github.com/dung204/taskapi/internal/task"
)

// answerCase prepares one way an invitation can fail to be answered: it returns
// who calls, the token they use and the time of the call.
type answerCase struct {
	name  string
	setup func(t *testing.T, e *svcEnv, tk task.Task) (actor uuid.UUID, token string, now time.Time)
}

func answerFailures() []answerCase {
	return []answerCase{
		{"unknown token", func(t *testing.T, e *svcEnv, _ task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			raw, _ := opaquetoken.Generate()
			return e.user(t).ID, opaquetoken.Encode(raw), time.Now()
		}},
		{"expired", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			u := e.user(t)
			inv := e.pending(t, tk.ID, u.Email, task.RoleViewer, time.Now())
			return u.ID, e.token(t, inv), inv.ExpiresAt.Add(time.Second)
		}},
		{"exactly at the expiry", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			u := e.user(t)
			inv := e.pending(t, tk.ID, u.Email, task.RoleViewer, time.Now())
			return u.ID, e.token(t, inv), inv.ExpiresAt
		}},
		{"declined", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			u := e.user(t)
			return u.ID, e.token(t, e.closed(t, tk.ID, u.Email, task.InvitationStatusDeclined)), time.Now()
		}},
		{"cancelled", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			u := e.user(t)
			return u.ID, e.token(t, e.closed(t, tk.ID, u.Email, task.InvitationStatusCancelled)), time.Now()
		}},
		{"already accepted", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			u := e.user(t)
			return u.ID, e.token(t, e.closed(t, tk.ID, u.Email, task.InvitationStatusAccepted)), time.Now()
		}},
		{"another verified account", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			invited := e.user(t)
			inv := e.pending(t, tk.ID, invited.Email, task.RoleViewer, time.Now())
			return e.user(t).ID, e.token(t, inv), time.Now()
		}},
		{"the invited address belongs to an unverified account", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			u := e.unverifiedUser(t)
			inv := e.pending(t, tk.ID, u.Email, task.RoleViewer, time.Now())
			return u.ID, e.token(t, inv), time.Now()
		}},
		{"nobody has the invited address", func(t *testing.T, e *svcEnv, tk task.Task) (uuid.UUID, string, time.Time) {
			t.Helper()
			inv := e.pending(t, tk.ID, "ghost-"+e.user(t).Email, task.RoleViewer, time.Now())
			return e.user(t).ID, e.token(t, inv), time.Now()
		}},
	}
}

func TestAcceptInvitation(t *testing.T) {
	t.Parallel()
	t.Run("success adds the member, closes the invitation and counts accepted", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)
		inv := e.pending(t, tk.ID, invitee.Email, task.RoleContributor, time.Now())
		token := e.token(t, inv)

		access, err := e.svc.AcceptInvitation(t.Context(), invitee.ID, token, time.Now())
		if err != nil {
			t.Fatalf("AcceptInvitation: %v", err)
		}
		if access.Task.ID != tk.ID || access.Role != task.RoleContributor {
			t.Errorf("access = task %v role %q, want %v as contributor", access.Task.ID, access.Role, tk.ID)
		}
		if got := e.invitationByToken(t, token).Status; got != task.InvitationStatusAccepted {
			t.Errorf("invitation status = %q, want accepted", got)
		}
		if got := e.invitationCount(t, "accepted"); got != 1 {
			t.Errorf("task_invitations_total{event=accepted} = %v, want 1", got)
		}
		if e.assignee(t, owner.ID, tk.ID) != nil {
			t.Errorf("the task got an assignee although the invitation did not ask for it")
		}
	})

	t.Run("assign_on_accept sets the assignee", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)
		inv := e.pending(t, tk.ID, invitee.Email, task.RoleViewer, time.Now())
		if _, err := e.pool.Exec(t.Context(), `UPDATE task_invitations SET assign_on_accept = true WHERE id = $1`, inv.ID); err != nil {
			t.Fatalf("set assign_on_accept: %v", err)
		}
		if _, err := e.svc.AcceptInvitation(t.Context(), invitee.ID, e.token(t, inv), time.Now()); err != nil {
			t.Fatalf("AcceptInvitation: %v", err)
		}
		if got := e.assignee(t, owner.ID, tk.ID); got == nil || *got != invitee.ID {
			t.Errorf("assignee = %v, want %v", got, invitee.ID)
		}
	})

	for _, c := range answerFailures() {
		t.Run(c.name+" is the unavailable error and counts nothing", func(t *testing.T) {
			t.Parallel()
			e := newSvcEnv(t)
			tk := e.task(t, e.user(t).ID)
			actor, token, now := c.setup(t, e, tk)
			_, err := e.svc.AcceptInvitation(t.Context(), actor, token, now)
			wantErr(t, err, task.ErrInvitationUnavailable)
			if got := e.invitationCount(t, "accepted"); got != 0 {
				t.Errorf("accepted counter = %v, want 0 after a failure", got)
			}
			if e.memberCount(t, tk.ID) != 0 {
				t.Errorf("a failed accept created a member")
			}
		})
	}

	t.Run("a malformed token is invalid", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		for _, tok := range []string{"", "not base64!", "c2hvcnQ"} {
			_, err := e.svc.AcceptInvitation(t.Context(), e.user(t).ID, tok, time.Now())
			wantErr(t, err, task.ErrInvalidToken)
		}
	})

	t.Run("a full task is 409, not the unavailable error", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)
		e.fillMembers(t, tk.ID, 50)
		token := e.token(t, e.pending(t, tk.ID, invitee.Email, task.RoleViewer, time.Now()))

		_, err := e.svc.AcceptInvitation(t.Context(), invitee.ID, token, time.Now())
		wantErr(t, err, task.ErrTaskFull)
		if got := e.invitationByToken(t, token).Status; got != task.InvitationStatusPending {
			t.Errorf("invitation status = %q, want it to stay pending", got)
		}
	})

	t.Run("the 50th member can still accept", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)
		e.fillMembers(t, tk.ID, 49)
		token := e.token(t, e.pending(t, tk.ID, invitee.Email, task.RoleViewer, time.Now()))
		if _, err := e.svc.AcceptInvitation(t.Context(), invitee.ID, token, time.Now()); err != nil {
			t.Fatalf("AcceptInvitation at 49 members: %v", err)
		}
	})
}

func TestDeclineInvitation(t *testing.T) {
	t.Parallel()
	t.Run("success closes the invitation and counts declined", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		invitee := e.user(t)
		tk := e.task(t, e.user(t).ID)
		token := e.token(t, e.pending(t, tk.ID, invitee.Email, task.RoleViewer, time.Now()))

		if err := e.svc.DeclineInvitation(t.Context(), invitee.ID, token, time.Now()); err != nil {
			t.Fatalf("DeclineInvitation: %v", err)
		}
		if got := e.invitationByToken(t, token).Status; got != task.InvitationStatusDeclined {
			t.Errorf("status = %q, want declined", got)
		}
		if e.memberCount(t, tk.ID) != 0 {
			t.Errorf("declining created a member")
		}
		if got := e.invitationCount(t, "declined"); got != 1 {
			t.Errorf("declined counter = %v, want 1", got)
		}
	})

	for _, c := range answerFailures() {
		t.Run(c.name+" is the unavailable error and counts nothing", func(t *testing.T) {
			t.Parallel()
			e := newSvcEnv(t)
			tk := e.task(t, e.user(t).ID)
			actor, token, now := c.setup(t, e, tk)
			wantErr(t, e.svc.DeclineInvitation(t.Context(), actor, token, now), task.ErrInvitationUnavailable)
			if got := e.invitationCount(t, "declined"); got != 0 {
				t.Errorf("declined counter = %v, want 0 after a failure", got)
			}
		})
	}

	t.Run("a malformed token is invalid", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		wantErr(t, e.svc.DeclineInvitation(t.Context(), e.user(t).ID, "###", time.Now()), task.ErrInvalidToken)
	})
}

func TestCancelInvitation(t *testing.T) {
	t.Parallel()
	t.Run("success closes the invitation and counts cancelled", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		inv := e.pending(t, tk.ID, "x-"+e.user(t).Email, task.RoleViewer, time.Now())
		token := e.token(t, inv)

		if err := e.svc.CancelInvitation(t.Context(), owner.ID, tk.ID, inv.ID, time.Now()); err != nil {
			t.Fatalf("CancelInvitation: %v", err)
		}
		if got := e.invitationByToken(t, token).Status; got != task.InvitationStatusCancelled {
			t.Errorf("status = %q, want cancelled", got)
		}
		if got := e.invitationCount(t, "cancelled"); got != 1 {
			t.Errorf("cancelled counter = %v, want 1", got)
		}
	})

	t.Run("an invitation of another task is not found and stays pending", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		mine, other := e.task(t, owner.ID), e.task(t, owner.ID)
		inv := e.pending(t, other.ID, "x-"+e.user(t).Email, task.RoleViewer, time.Now())

		wantErr(t, e.svc.CancelInvitation(t.Context(), owner.ID, mine.ID, inv.ID, time.Now()), task.ErrInvitationNotFound)
		shares, _ := e.svc.ListShares(t.Context(), owner.ID, other.ID, time.Now())
		if len(shares) != 1 {
			t.Errorf("shares of the other task = %d, want 1 (untouched)", len(shares))
		}
		if got := e.invitationCount(t, "cancelled"); got != 0 {
			t.Errorf("cancelled counter = %v, want 0", got)
		}
	})

	t.Run("a member who cannot manage shares is forbidden and counted", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		actor := e.member(t, tk.ID, task.RoleManager)
		inv := e.pending(t, tk.ID, "x-"+e.user(t).Email, task.RoleViewer, time.Now())
		wantErr(t, e.svc.CancelInvitation(t.Context(), actor.ID, tk.ID, inv.ID, time.Now()), task.ErrCannotManageShares)
		if got := e.denied(t, "manage_shares"); got != 1 {
			t.Errorf("denied counter = %v, want 1", got)
		}
	})

	t.Run("a stranger gets not found", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		inv := e.pending(t, tk.ID, "x-"+e.user(t).Email, task.RoleViewer, time.Now())
		wantErr(t, e.svc.CancelInvitation(t.Context(), e.user(t).ID, tk.ID, inv.ID, time.Now()), task.ErrNotFound)
	})
}
