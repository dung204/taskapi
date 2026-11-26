package email

import (
	"context"
	"errors"
	"testing"
)

type stubBuilder struct {
	msg Message
	err error
	got []Kind
}

func (b *stubBuilder) Build(_ context.Context, item ClaimedItem) (Message, error) {
	b.got = append(b.got, item.Kind)
	return b.msg, b.err
}

func TestRouter_Build(t *testing.T) {
	a := &stubBuilder{msg: Message{Subject: "a"}}
	b := &stubBuilder{err: ErrNothingToSend}
	r := Router{KindVerifyEmail: a, KindTaskAdded: b}

	msg, err := r.Build(t.Context(), ClaimedItem{Kind: KindVerifyEmail})
	if err != nil || msg.Subject != "a" {
		t.Fatalf("verify_email: msg = %+v, err = %v", msg, err)
	}

	// The error of the chosen builder is returned unchanged.
	if _, err := r.Build(t.Context(), ClaimedItem{Kind: KindTaskAdded}); !errors.Is(err, ErrNothingToSend) {
		t.Errorf("task_added: err = %v, want %v", err, ErrNothingToSend)
	}

	if len(a.got) != 1 || len(b.got) != 1 {
		t.Errorf("calls = %d and %d, want 1 and 1", len(a.got), len(b.got))
	}

	if _, err := r.Build(t.Context(), ClaimedItem{Kind: KindResetPassword}); err == nil || errors.Is(err, ErrNothingToSend) {
		t.Errorf("unregistered kind: err = %v, want a failure", err)
	}
}
