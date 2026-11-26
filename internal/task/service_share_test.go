package task_test

import (
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

func TestListShares(t *testing.T) {
	t.Parallel()
	t.Run("members and unexpired pending invitations, ordered by created_at then id", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		now := time.Now()

		e.pending(t, tk.ID, "expired-"+e.user(t).Email, task.RoleViewer, now.Add(-31*24*time.Hour)) // not listed
		e.closed(t, tk.ID, "declined-"+e.user(t).Email, task.InvitationStatusDeclined)              // not listed
		first := e.pending(t, tk.ID, "a-"+e.user(t).Email, task.RoleViewer, now.Add(-2*time.Hour))
		m := e.member(t, tk.ID, task.RoleEditor)
		last := e.pending(t, tk.ID, "z-"+e.user(t).Email, task.RoleViewer, now.Add(time.Hour))

		shares, err := e.svc.ListShares(t.Context(), owner.ID, tk.ID, now)
		if err != nil {
			t.Fatalf("ListShares: %v", err)
		}
		var got []uuid.UUID
		for _, s := range shares {
			got = append(got, s.ID)
		}
		want := []uuid.UUID{first.ID, m.ID, last.ID}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
			t.Errorf("shares = %v, want %v", got, want)
		}
		for i := 1; i < len(shares); i++ {
			if shares[i].CreatedAt.Before(shares[i-1].CreatedAt) {
				t.Errorf("shares are not ordered by created_at at index %d", i)
			}
		}
	})

	t.Run("a pending invitation expiring exactly now is not listed", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		inv := e.pending(t, tk.ID, "x-"+e.user(t).Email, task.RoleViewer, time.Now())
		shares, err := e.svc.ListShares(t.Context(), owner.ID, tk.ID, inv.ExpiresAt)
		if err != nil || len(shares) != 0 {
			t.Errorf("ListShares at the expiry = (%v, %v), want empty", shares, err)
		}
	})

	t.Run("a stranger gets not found, never an empty list", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		shares, err := e.svc.ListShares(t.Context(), e.user(t).ID, tk.ID, time.Now())
		wantErr(t, err, task.ErrNotFound)
		if shares != nil {
			t.Errorf("shares = %v, want nil with the error", shares)
		}
	})

	t.Run("no shares is an empty non-nil slice", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		shares, err := e.svc.ListShares(t.Context(), owner.ID, tk.ID, time.Now())
		if err != nil || shares == nil || len(shares) != 0 {
			t.Errorf("ListShares = (%#v, %v), want an empty non-nil slice", shares, err)
		}
	})

	t.Run("a viewer member may list", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		viewer := e.member(t, tk.ID, task.RoleViewer)
		if _, err := e.svc.ListShares(t.Context(), viewer.ID, tk.ID, time.Now()); err != nil {
			t.Errorf("ListShares as a viewer: %v", err)
		}
	})
}

func TestAssign(t *testing.T) {
	t.Parallel()
	t.Run("the owner and a member can be assigned, a stranger and an unknown email cannot", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		member := e.member(t, tk.ID, task.RoleViewer)
		stranger := e.user(t)

		for _, ok := range []string{owner.Email, member.Email} {
			if _, err := e.svc.Assign(t.Context(), owner.ID, tk.ID, ok); err != nil {
				t.Errorf("Assign %s: %v", ok, err)
			}
		}
		for _, bad := range []string{stranger.Email, "ghost-" + stranger.Email} {
			_, err := e.svc.Assign(t.Context(), owner.ID, tk.ID, bad)
			wantErr(t, err, task.ErrAssigneeNotMember)
		}
		if got := e.assignee(t, owner.ID, tk.ID); got == nil || *got != member.ID {
			t.Errorf("assignee = %v, want the last valid one %v", got, member.ID)
		}
	})

	t.Run("a role without the assign permission is forbidden and counted", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		actor := e.member(t, tk.ID, task.RoleEditor)
		_, err := e.svc.Assign(t.Context(), actor.ID, tk.ID, actor.Email)
		wantErr(t, err, task.ErrCannotAssign)
		if got := e.denied(t, "assign"); got != 1 {
			t.Errorf("denied counter = %v, want 1", got)
		}
	})
}

func TestRemoveMemberAndLeave(t *testing.T) {
	t.Parallel()
	t.Run("removing a member clears their assignment", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		m := e.member(t, tk.ID, task.RoleViewer)
		if _, err := e.svc.Assign(t.Context(), owner.ID, tk.ID, m.Email); err != nil {
			t.Fatalf("Assign: %v", err)
		}
		if err := e.svc.RemoveMember(t.Context(), owner.ID, tk.ID, m.ID); err != nil {
			t.Fatalf("RemoveMember: %v", err)
		}
		if got := e.assignee(t, owner.ID, tk.ID); got != nil {
			t.Errorf("assignee = %v, want nil after the member was removed", *got)
		}
	})

	t.Run("removing someone else's assignment is left alone", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		a, b := e.member(t, tk.ID, task.RoleViewer), e.member(t, tk.ID, task.RoleViewer)
		if _, err := e.svc.Assign(t.Context(), owner.ID, tk.ID, a.Email); err != nil {
			t.Fatalf("Assign: %v", err)
		}
		if err := e.svc.RemoveMember(t.Context(), owner.ID, tk.ID, b.ID); err != nil {
			t.Fatalf("RemoveMember: %v", err)
		}
		if got := e.assignee(t, owner.ID, tk.ID); got == nil || *got != a.ID {
			t.Errorf("assignee = %v, want %v", got, a.ID)
		}
	})

	t.Run("an unknown member is not found; a non-owner cannot remove", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		wantErr(t, e.svc.RemoveMember(t.Context(), owner.ID, tk.ID, e.user(t).ID), task.ErrMemberNotFound)
		m := e.member(t, tk.ID, task.RoleManager)
		wantErr(t, e.svc.RemoveMember(t.Context(), m.ID, tk.ID, m.ID), task.ErrCannotManageShares)
	})

	t.Run("leaving removes the caller and clears their assignment", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		m := e.member(t, tk.ID, task.RoleViewer)
		if _, err := e.svc.Assign(t.Context(), owner.ID, tk.ID, m.Email); err != nil {
			t.Fatalf("Assign: %v", err)
		}
		if err := e.svc.Leave(t.Context(), m.ID, tk.ID); err != nil {
			t.Fatalf("Leave: %v", err)
		}
		if e.memberCount(t, tk.ID) != 0 {
			t.Errorf("the member is still there")
		}
		if got := e.assignee(t, owner.ID, tk.ID); got != nil {
			t.Errorf("assignee = %v, want nil after leaving", *got)
		}
	})

	t.Run("a stranger cannot leave", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		wantErr(t, e.svc.Leave(t.Context(), e.user(t).ID, tk.ID), task.ErrNotFound)
	})
}

func TestUpdate_DeniedMetric(t *testing.T) {
	t.Parallel()
	e := newSvcEnv(t)
	tk := e.task(t, e.user(t).ID)
	viewer := e.member(t, tk.ID, task.RoleViewer)

	title, status := "new", task.StatusDone
	_, err := e.svc.Update(t.Context(), viewer.ID, tk.ID, task.Patch{Title: &title, Status: &status}, time.Now())
	wantErr(t, err, task.ErrCannotEditContent)
	if got := e.denied(t, "edit_content"); got != 1 {
		t.Errorf("edit_content = %v, want 1", got)
	}
	if got := e.denied(t, "change_status"); got != 0 {
		t.Errorf("change_status = %v, want 0 (one denial per request)", got)
	}
}

// --- races: two goroutines on real Postgres ----------------------------------

// race runs a and b at the same time and returns their errors.
func race(a, b func() error) (errA, errB error) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() { defer wg.Done(); <-start; errA = a() }()
	go func() { defer wg.Done(); <-start; errB = b() }()
	close(start)
	wg.Wait()
	return errA, errB
}

const raceRounds = 10

func TestRace_LastFreeMemberSlot(t *testing.T) {
	t.Parallel()
	t.Run("two direct adds", func(t *testing.T) {
		for range raceRounds {
			e := newSvcEnv(t)
			owner := e.user(t)
			tk := e.task(t, owner.ID)
			e.fillMembers(t, tk.ID, 49)
			u1, u2 := e.user(t), e.user(t)
			invite := func(u string) func() error {
				return func() error {
					_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: u, Role: task.RoleViewer}, time.Now())
					return err
				}
			}
			errA, errB := race(invite(u1.Email), invite(u2.Email))
			wantOneWinner(t, errA, errB, task.ErrMembersExceeded)
			if n := e.memberCount(t, tk.ID); n != 50 {
				t.Fatalf("members = %d, want exactly 50", n)
			}
		}
	})

	t.Run("two accepts", func(t *testing.T) {
		for range raceRounds {
			e := newSvcEnv(t)
			tk := e.task(t, e.user(t).ID)
			e.fillMembers(t, tk.ID, 49)
			u1, u2 := e.user(t), e.user(t)
			t1 := e.token(t, e.pending(t, tk.ID, u1.Email, task.RoleViewer, time.Now()))
			t2 := e.token(t, e.pending(t, tk.ID, u2.Email, task.RoleViewer, time.Now()))
			accept := func(u uuid.UUID, tok string) func() error {
				return func() error {
					_, err := e.svc.AcceptInvitation(t.Context(), u, tok, time.Now())
					return err
				}
			}
			errA, errB := race(accept(u1.ID, t1), accept(u2.ID, t2))
			wantOneWinner(t, errA, errB, task.ErrTaskFull)
			if n := e.memberCount(t, tk.ID); n != 50 {
				t.Fatalf("members = %d, want exactly 50", n)
			}
		}
	})
}

// wantOneWinner checks that exactly one of the two calls succeeded and the other
// failed with want.
func wantOneWinner(t *testing.T, errA, errB, want error) {
	t.Helper()
	switch {
	case errA == nil && errB == nil:
		t.Fatalf("both calls succeeded, want one to fail with %v", want)
	case errA != nil && errB != nil:
		t.Fatalf("both calls failed: %v, %v", errA, errB)
	case errA != nil:
		wantErr(t, errA, want)
	default:
		wantErr(t, errB, want)
	}
}

func TestRace_AssignVersusRemoveMember(t *testing.T) {
	t.Parallel()
	for range raceRounds * 3 {
		e := newSvcEnv(t)
		owner := e.user(t)
		tk := e.task(t, owner.ID)
		m := e.member(t, tk.ID, task.RoleViewer)

		race(
			func() error { _, err := e.svc.Assign(t.Context(), owner.ID, tk.ID, m.Email); return err },
			func() error { return e.svc.RemoveMember(t.Context(), owner.ID, tk.ID, m.ID) },
		)

		if got := e.assignee(t, owner.ID, tk.ID); got != nil {
			if _, err := e.members.Get(t.Context(), tk.ID, *got); err != nil && *got != owner.ID {
				t.Fatalf("the assignee %v is not a member any more: %v", *got, err)
			}
		}
	}
}

func TestRace_AcceptVersusDirectAdd(t *testing.T) {
	t.Parallel()
	for range raceRounds {
		e := newSvcEnv(t)
		owner, invitee := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)
		token := e.token(t, e.pending(t, tk.ID, invitee.Email, task.RoleViewer, time.Now()))

		errAccept, errInvite := race(
			func() error { _, err := e.svc.AcceptInvitation(t.Context(), invitee.ID, token, time.Now()); return err },
			func() error {
				_, _, err := e.svc.Invite(t.Context(), owner.ID, tk.ID, task.InviteParams{Email: invitee.Email, Role: task.RoleViewer}, time.Now())
				return err
			},
		)

		if errAccept != nil {
			wantErr(t, errAccept, task.ErrInvitationUnavailable) // the direct add got there first
		}
		if errInvite != nil {
			wantErr(t, errInvite, task.ErrMemberExists) // the accept got there first
		}
		if errAccept != nil && errInvite != nil {
			t.Fatalf("both failed: %v, %v", errAccept, errInvite)
		}
		if n := e.memberCount(t, tk.ID); n != 1 {
			t.Fatalf("members = %d, want exactly one", n)
		}
		var pending int
		if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FROM task_invitations WHERE task_id = $1 AND status = 'pending'`, tk.ID).Scan(&pending); err != nil || pending != 0 {
			t.Fatalf("pending invitations left = %d (err %v), want 0", pending, err)
		}
	}
}
