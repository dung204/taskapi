package task_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/opaquetoken"
	"github.com/dung204/taskapi/internal/task"
)

const baseURL = "https://app.example.com"

// failingInvitations wraps the real store and fails the calls whose error is set.
type failingInvitations struct {
	task.InvitationStore
	getForMail, setTokenHash error
}

func (f failingInvitations) GetForMail(ctx context.Context, id uuid.UUID) (task.InvitationMail, error) {
	if f.getForMail != nil {
		return task.InvitationMail{}, f.getForMail
	}
	return f.InvitationStore.GetForMail(ctx, id)
}

func (f failingInvitations) SetTokenHash(ctx context.Context, id uuid.UUID, h []byte, now time.Time) error {
	if f.setTokenHash != nil {
		return f.setTokenHash
	}
	return f.InvitationStore.SetTokenHash(ctx, id, h, now)
}

func (e *svcEnv) builder(t *testing.T, inv task.InvitationStore, now func() time.Time) *task.EmailBuilder {
	t.Helper()
	if inv == nil {
		inv = e.invitations
	}
	b, err := task.NewEmailBuilder(task.EmailBuilderDeps{BaseURL: baseURL, Tasks: e.tasks, Invitations: inv, Now: now})
	if err != nil {
		t.Fatalf("NewEmailBuilder: %v", err)
	}
	return b
}

func item(kind email.Kind, to string, ref *uuid.UUID) email.ClaimedItem {
	return email.ClaimedItem{ID: uuid.NewV7(), Kind: kind, ToEmail: to, RefID: ref}
}

var tokenInLink = regexp.MustCompile(`/invitations/accept\?token=([A-Za-z0-9_-]+)`)

// tokenOf extracts the token from the accept link of a message.
func tokenOf(t *testing.T, m email.Message) string {
	t.Helper()
	for _, body := range []string{m.Text, m.HTML} {
		if sub := tokenInLink.FindStringSubmatch(body); sub != nil {
			return sub[1]
		}
	}
	t.Fatalf("no accept link in the message:\n%s", m.Text)
	return ""
}

func TestEmailBuilder_Build(t *testing.T) {
	t.Parallel()
	t.Run("an invitation mail carries a fresh token whose hash is stored", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		inv := e.pending(t, tk.ID, "guest@example.com", task.RoleEditor, time.Now())

		msg, err := e.builder(t, nil, nil).Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if msg.To != "guest@example.com" || msg.Subject == "" {
			t.Errorf("message = to %q subject %q, want the recipient and a subject", msg.To, msg.Subject)
		}
		if !strings.Contains(msg.Text, tk.Title) {
			t.Errorf("the text does not mention the task title:\n%s", msg.Text)
		}
		got := e.invitationByToken(t, tokenOf(t, msg))
		if got.ID != inv.ID {
			t.Errorf("the token in the link points at %v, want %v", got.ID, inv.ID)
		}
	})

	t.Run("an invitation mail states the 30 day lifetime in words, not as a raw duration", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		inv := e.pending(t, tk.ID, "guest@example.com", task.RoleViewer, time.Now())

		msg, err := e.builder(t, nil, nil).Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		for name, body := range map[string]string{"text": msg.Text, "html": msg.HTML} {
			if !strings.Contains(body, "expires in 4 weeks 2 days") {
				t.Errorf("the %s body does not say \"expires in 4 weeks 2 days\":\n%s", name, body)
			}
			if strings.Contains(body, "720h") {
				t.Errorf("the %s body shows the raw duration:\n%s", name, body)
			}
		}
	})

	t.Run("rebuilding a mail invalidates the previous token", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		inv := e.pending(t, tk.ID, "guest@example.com", task.RoleViewer, time.Now())
		b := e.builder(t, nil, nil)

		first, err := b.Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID))
		if err != nil {
			t.Fatalf("first Build: %v", err)
		}
		second, err := b.Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID))
		if err != nil {
			t.Fatalf("second Build: %v", err)
		}
		oldToken, newToken := tokenOf(t, first), tokenOf(t, second)
		if oldToken == newToken {
			t.Fatalf("both mails carry the same token")
		}
		raw, _ := opaquetoken.Decode(oldToken)
		if _, err := e.invitations.GetByTokenHash(t.Context(), opaquetoken.Hash(raw)); !errors.Is(err, task.ErrInvitationNotFound) {
			t.Errorf("the old token still works: %v", err)
		}
		if got := e.invitationByToken(t, newToken); got.ID != inv.ID {
			t.Errorf("the new token points at %v, want %v", got.ID, inv.ID)
		}
	})

	t.Run("a task_added mail links to the task and touches no token", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		owner, member := e.user(t), e.user(t)
		tk := e.task(t, owner.ID)
		inv := e.closed(t, tk.ID, member.Email, task.InvitationStatusAccepted)

		msg, err := e.builder(t, nil, nil).Build(t.Context(), item(email.KindTaskAdded, member.Email, &inv.ID))
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		if !strings.Contains(msg.Text, baseURL+"/task/"+tk.ID.String()) {
			t.Errorf("no link to the task in:\n%s", msg.Text)
		}
		if tokenInLink.MatchString(msg.Text) || tokenInLink.MatchString(msg.HTML) {
			t.Errorf("a task_added mail must not carry an accept link")
		}
	})
}

func TestEmailBuilder_NothingToSend(t *testing.T) {
	t.Parallel()
	t.Run("the invitation row is gone", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		ref := uuid.NewV7()
		for _, k := range []email.Kind{email.KindTaskInvitation, email.KindTaskAdded} {
			_, err := e.builder(t, nil, nil).Build(t.Context(), item(k, "guest@example.com", &ref))
			wantErr(t, err, email.ErrNothingToSend)
		}
	})

	t.Run("the invitation is no longer pending", func(t *testing.T) {
		for _, st := range []task.InvitationStatus{task.InvitationStatusDeclined, task.InvitationStatusCancelled, task.InvitationStatusAccepted} {
			e := newSvcEnv(t)
			tk := e.task(t, e.user(t).ID)
			inv := e.closed(t, tk.ID, "guest@example.com", st)
			_, err := e.builder(t, nil, nil).Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID))
			if !errors.Is(err, email.ErrNothingToSend) {
				t.Errorf("%s: error = %v, want ErrNothingToSend", st, err)
			}
		}
	})

	t.Run("the invitation has expired, exactly at the expiry included", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		inv := e.pending(t, tk.ID, "guest@example.com", task.RoleViewer, time.Now())
		for _, at := range []time.Time{inv.ExpiresAt, inv.ExpiresAt.Add(time.Hour)} {
			_, err := e.builder(t, nil, func() time.Time { return at }).Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID))
			if !errors.Is(err, email.ErrNothingToSend) {
				t.Errorf("at %v: error = %v, want ErrNothingToSend", at, err)
			}
		}
		// One microsecond earlier it is still sent.
		at := inv.ExpiresAt.Add(-time.Microsecond)
		if _, err := e.builder(t, nil, func() time.Time { return at }).Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID)); err != nil {
			t.Errorf("just before the expiry: %v", err)
		}
	})
}

func TestEmailBuilder_Errors(t *testing.T) {
	t.Parallel()
	boom := errors.New("database is down")

	t.Run("any other store error is returned so the worker retries", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		ref := uuid.NewV7()
		_, err := e.builder(t, failingInvitations{InvitationStore: e.invitations, getForMail: boom}, nil).
			Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &ref))
		wantErr(t, err, boom)
		if errors.Is(err, email.ErrNothingToSend) {
			t.Errorf("a failing store must not be reported as nothing to send")
		}
	})

	t.Run("a missing RefID is a plain error, not nothing to send", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		for _, k := range []email.Kind{email.KindTaskInvitation, email.KindTaskAdded} {
			_, err := e.builder(t, nil, nil).Build(t.Context(), item(k, "guest@example.com", nil))
			if err == nil || errors.Is(err, email.ErrNothingToSend) {
				t.Errorf("%s: error = %v, want a plain error", k, err)
			}
		}
	})

	t.Run("if storing the token hash fails no message is returned", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		inv := e.pending(t, tk.ID, "guest@example.com", task.RoleViewer, time.Now())
		msg, err := e.builder(t, failingInvitations{InvitationStore: e.invitations, setTokenHash: boom}, nil).
			Build(t.Context(), item(email.KindTaskInvitation, "guest@example.com", &inv.ID))
		wantErr(t, err, boom)
		if msg != (email.Message{}) {
			t.Errorf("message = %+v, want none: a mail without a stored token would carry a dead link", msg)
		}
	})

	t.Run("an unsupported kind is a plain error", func(t *testing.T) {
		t.Parallel()
		e := newSvcEnv(t)
		tk := e.task(t, e.user(t).ID)
		inv := e.pending(t, tk.ID, "guest@example.com", task.RoleViewer, time.Now())
		_, err := e.builder(t, nil, nil).Build(t.Context(), item(email.KindVerifyEmail, "guest@example.com", &inv.ID))
		if err == nil || errors.Is(err, email.ErrNothingToSend) {
			t.Errorf("error = %v, want a plain error", err)
		}
	})
}
