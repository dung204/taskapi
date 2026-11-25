package task_test

import (
	"testing"
	"time"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/task"
)

func TestInvite_Branches(t *testing.T) {
	t.Parallel()
	t.Run("a verified account is added directly and gets task_added", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)

		share, retry, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: invitee.Email, Role: task.RoleEditor}, time.Now())
		if err != nil || retry != 0 {
			t.Fatalf("Invite = (%v, %v), want success", retry, err)
		}
		if share.Kind != task.ShareKindMember || share.ID != invitee.ID || share.Role != task.RoleEditor {
			t.Errorf("share = %+v, want member %v as editor", share, invitee.ID)
		}
		if _, err := e.members.Get(t.Context(), tk.ID, invitee.ID); err != nil {
			t.Errorf("the invitee is not a member: %v", err)
		}
		rows := e.outbox(t, invitee.Email)
		if len(rows) != 1 || rows[0].Kind != string(email.KindTaskAdded) {
			t.Errorf("outbox = %+v, want one %s", rows, email.KindTaskAdded)
		}
		if got := e.invitationCount(t, "added"); got != 1 {
			t.Errorf("task_invitations_total{event=added} = %v, want 1", got)
		}
		if got := e.invitationCount(t, "created"); got != 0 {
			t.Errorf("task_invitations_total{event=created} = %v, want 0 for a direct add", got)
		}
	})

	for name, mk := range map[string]func(*testing.T, *svcEnv) string{
		"no account": func(t *testing.T, e *svcEnv) string {
			t.Helper()
			return "nobody-" + e.user(t).Email
		},
		"unverified account": func(t *testing.T, e *svcEnv) string {
			t.Helper()
			return e.unverifiedUser(t).Email
		},
		"disabled account": func(t *testing.T, e *svcEnv) string {
			t.Helper()
			return e.disabledUser(t).Email
		},
	} {
		t.Run(name+" gets a pending invitation and task_invitation", func(t *testing.T) {
			t.Parallel()
			e := newSvcEnv(t)
			owner := e.user(t)
			tk := e.task(t, owner.ID)
			addr := mk(t, e)

			share, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: addr, Role: task.RoleViewer}, time.Now())
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if share.Kind != task.ShareKindInvitation || share.ExpiresAt == nil {
				t.Errorf("share = %+v, want a pending invitation with an expiry", share)
			}
			rows := e.outbox(t, addr)
			if len(rows) != 1 || rows[0].Kind != string(email.KindTaskInvitation) || rows[0].RefID == nil || *rows[0].RefID != share.ID {
				t.Errorf("outbox = %+v, want one %s referring to the invitation", rows, email.KindTaskInvitation)
			}
			if got := e.invitationCount(t, "created"); got != 1 {
				t.Errorf("task_invitations_total{event=created} = %v, want 1", got)
			}
		})
	}

	t.Run("the email is matched case-insensitively", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)

		share, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: "  " + upper(invitee.Email) + " ", Role: task.RoleViewer}, time.Now())
		if err != nil {
			t.Fatalf("Invite: %v", err)
		}
		if share.Kind != task.ShareKindMember {
			t.Errorf("kind = %q, want member (the account exists)", share.Kind)
		}
	})

	t.Run("assign on a direct add sets the assignee", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)

		if _, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: invitee.Email, Role: task.RoleViewer, Assign: true}, time.Now()); err != nil {
			t.Fatalf("Invite: %v", err)
		}
		if got := e.assignee(t, owner.ID, tk.ID); got == nil || *got != invitee.ID {
			t.Errorf("assignee = %v, want %v", got, invitee.ID)
		}
	})

	t.Run("a direct add cancels the pending invitation of the same email", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		invitee := e.user(t)
		old := e.pending(t, tk.ID, invitee.Email, task.RoleViewer, time.Now())

		if _, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: invitee.Email, Role: task.RoleEditor}, time.Now()); err != nil {
			t.Fatalf("Invite: %v", err)
		}

		shares, err := e.svc.ListShares(t.Context(), owner.ID, tk.ID, time.Now())
		if err != nil {
			t.Fatalf("ListShares: %v", err)
		}
		if len(shares) != 1 || shares[0].Kind != task.ShareKindMember {
			t.Errorf("shares = %+v, want only the member (old invitation %v cancelled)", shares, old.ID)
		}
		if got := e.invitationCount(t, "cancelled"); got != 1 {
			t.Errorf("task_invitations_total{event=cancelled} = %v, want 1", got)
		}
	})
}

func TestInvite_BusinessErrors(t *testing.T) {
	t.Parallel()
	t.Run("inviting yourself is 422", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: owner.Email, Role: task.RoleViewer}, time.Now())
		wantErr(t, err, task.ErrCannotInviteSelf)
		wantNoBudgetUsed(t, e)
	})

	t.Run("an existing member is 409", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		m := e.member(t, tk.ID, task.RoleViewer)
		_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: m.Email, Role: task.RoleEditor}, time.Now())
		wantErr(t, err, task.ErrMemberExists)
		wantNoBudgetUsed(t, e)
	})

	t.Run("a pending invitation for the same email is 409", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		addr := "late-" + e.user(t).Email
		e.pending(t, tk.ID, addr, task.RoleViewer, time.Now())
		_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: addr, Role: task.RoleViewer}, time.Now())
		wantErr(t, err, task.ErrInvitationExists)
		wantNoBudgetUsed(t, e)
	})

	t.Run("an expired pending invitation does not block a new one and is deleted", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		addr := "late-" + e.user(t).Email
		old := e.pending(t, tk.ID, addr, task.RoleViewer, time.Now().Add(-31*24*time.Hour))

		share, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: addr, Role: task.RoleViewer}, time.Now())
		if err != nil {
			t.Fatalf("Invite: %v", err)
		}
		if share.ID == old.ID {
			t.Errorf("the new invitation reuses the expired id")
		}
		if got := e.invitationCount(t, "expired"); got != 1 {
			t.Errorf("task_invitations_total{event=expired} = %v, want 1", got)
		}
		var n int
		if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FROM task_invitations WHERE id = $1`, old.ID).Scan(&n); err != nil || n != 0 {
			t.Errorf("expired invitation rows = %d (err %v), want 0", n, err)
		}
	})

	t.Run("a role that cannot be granted is 422 before any lookup or limiter", func(t *testing.T) {
		for _, role := range []task.Role{task.RoleOwner, "admin", ""} {
			e := newSvcEnv(t)
			owner := e.user(t)
			tk := e.task(t, owner.ID)
			_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: "not an email", Role: role}, time.Now())
			wantErr(t, err, task.ErrInvalidRole) // the role is checked before the email is even parsed
			wantNoBudgetUsed(t, e)
		}
	})
}

func TestInvite_Permissions(t *testing.T) {
	t.Parallel()
	for _, role := range []task.Role{task.RoleViewer, task.RoleContributor, task.RoleEditor, task.RoleManager} {
		t.Run(string(role)+" cannot manage shares", func(t *testing.T) {
			t.Parallel()
			e := newSvcEnv(t)
			owner := e.user(t)
			tk := e.task(t, owner.ID)
			actor := e.member(t, tk.ID, role)
			_, _, err := e.svc.Invite(t.Context(), actor.ID, tk.ID, task.InviteParams{Email: e.user(t).Email, Role: task.RoleViewer}, time.Now())
			wantErr(t, err, task.ErrCannotManageShares)
			wantNoBudgetUsed(t, e)
			if got := e.denied(t, "manage_shares"); got != 1 {
				t.Errorf("task_authz_denied_total{permission=manage_shares} = %v, want 1", got)
			}
		})
	}

	t.Run("a stranger gets not found, not forbidden", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, stranger := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)
		_, _, err := e.svc.Invite(t.Context(), stranger.ID, tk.ID, task.InviteParams{Email: e.user(t).Email, Role: task.RoleViewer}, time.Now())
		wantErr(t, err, task.ErrNotFound)
		if got := e.denied(t, "manage_shares"); got != 0 {
			t.Errorf("a stranger must not count as an authorization denial, got %v", got)
		}
	})
}

func TestInvite_Caps(t *testing.T) {
	t.Parallel()
	t.Run("members: 49 plus one is allowed, the 51st is 422", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		e.fillMembers(t, tk.ID, 49)

		if _, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: e.user(t).Email, Role: task.RoleViewer}, time.Now()); err != nil {
			t.Fatalf("the 50th member: %v", err)
		}
		_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: e.user(t).Email, Role: task.RoleViewer}, time.Now())
		wantErr(t, err, task.ErrMembersExceeded)
		if n := e.memberCount(t, tk.ID); n != 50 {
			t.Errorf("members = %d, want 50", n)
		}
	})

	t.Run("pending invitations per owner: 199 plus one is allowed, the 201st is 422", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		for i := range 199 {
			e.pending(t, tk.ID, "cap-"+uuidString(i)+"@example.com", task.RoleViewer, time.Now())
		}
		if _, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: "cap-200@example.com", Role: task.RoleViewer}, time.Now()); err != nil {
			t.Fatalf("the 200th pending invitation: %v", err)
		}
		_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: "cap-201@example.com", Role: task.RoleViewer}, time.Now())
		wantErr(t, err, task.ErrPendingInvitationsExceeded)
	})

	t.Run("hourly owner cap: 429 with retry-after and nothing written", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		e.owner.retryAfter = 17 * time.Minute
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		addr := "cap-" + e.user(t).Email

		share, retry, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: addr, Role: task.RoleViewer}, time.Now())
		if err != nil || retry != 17*time.Minute || share != (task.Share{}) {
			t.Fatalf("Invite = (%+v, %v, %v), want (zero, 17m, nil)", share, retry, err)
		}
		if e.mail.Calls() != 0 {
			t.Errorf("the mail limiter was asked after the owner limiter refused")
		}
		if rows := e.outbox(t, addr); len(rows) != 0 {
			t.Errorf("outbox = %+v, want nothing", rows)
		}
		shares, _ := e.svc.ListShares(t.Context(), owner.ID, tk.ID, time.Now())
		if len(shares) != 0 {
			t.Errorf("shares = %+v, want none", shares)
		}
	})

	t.Run("a failing owner limiter fails open", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		bad := &fakeOwnerLimiter{err: errBackend}
		svc := e.build(task.NewFailOpenOwnerInviteLimiter(bad, discardLogger(), nil), e.mail)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		if _, _, err := svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: "x-" + e.user(t).Email, Role: task.RoleViewer}, time.Now()); err != nil {
			t.Fatalf("Invite with a failing limiter: %v", err)
		}
		if bad.Calls() != 1 {
			t.Errorf("limiter calls = %d, want 1", bad.Calls())
		}
	})

	t.Run("a blocking mail limiter still creates the invitation but queues no mail", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		e.mail.retryAfter = time.Minute
		owner := e.user(t)
		tk := e.task(t, owner.ID)

		for _, addr := range []string{"nomail-" + e.user(t).Email, e.user(t).Email} { // pending and direct add
			if _, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: addr, Role: task.RoleViewer}, time.Now()); err != nil {
				t.Fatalf("Invite %s: %v", addr, err)
			}
			if rows := e.outbox(t, addr); len(rows) != 0 {
				t.Errorf("outbox for %s = %+v, want none", addr, rows)
			}
		}
		shares, _ := e.svc.ListShares(t.Context(), owner.ID, tk.ID, time.Now())
		if len(shares) != 2 {
			t.Errorf("shares = %d, want 2", len(shares))
		}
	})
}

// wantNoBudgetUsed checks that a rejected request did not spend limiter budget.
func wantNoBudgetUsed(t *testing.T, e *svcEnv) {
	t.Helper()
	if e.owner.Calls() != 0 || e.mail.Calls() != 0 {
		t.Errorf("limiter calls = owner %d, mail %d, want none for a rejected request", e.owner.Calls(), e.mail.Calls())
	}
}
