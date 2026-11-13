package redis

import (
	"testing"
	"time"
	"uuid"
)

func TestRevocationStore(t *testing.T) {
	client := newTestClient(t)
	ctx := t.Context()
	at := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

	newStore := func() *RevocationStore { return NewRevocationStore(client, time.Minute) }

	t.Run("nothing is revoked by default", func(t *testing.T) {
		revoked, err := newStore().Check(ctx, uuid.NewV7(), uuid.NewV7(), at)
		if err != nil || revoked {
			t.Errorf("Check() = %v, %v, want false, nil", revoked, err)
		}
	})

	t.Run("a revoked session is rejected, other sessions are not", func(t *testing.T) {
		s, sid, other, uid := newStore(), uuid.NewV7(), uuid.NewV7(), uuid.NewV7()
		if err := s.RevokeSession(ctx, sid); err != nil {
			t.Fatalf("RevokeSession() = %v", err)
		}

		if revoked, err := s.Check(ctx, sid, uid, at); err != nil || !revoked {
			t.Errorf("Check(revoked session) = %v, %v, want true, nil", revoked, err)
		}
		if revoked, err := s.Check(ctx, other, uid, at); err != nil || revoked {
			t.Errorf("Check(other session) = %v, %v, want false, nil", revoked, err)
		}
	})

	t.Run("a revoked user rejects tokens issued before the revocation only", func(t *testing.T) {
		s, uid := newStore(), uuid.NewV7()
		if err := s.RevokeUser(ctx, uid, at); err != nil {
			t.Fatalf("RevokeUser() = %v", err)
		}

		for _, tt := range []struct {
			name string
			iat  time.Time
			want bool
		}{
			{"issued before", at.Add(-time.Second), true},
			{"issued at the same second", at, false},
			{"issued after", at.Add(time.Second), false},
		} {
			revoked, err := s.Check(ctx, uuid.NewV7(), uid, tt.iat)
			if err != nil || revoked != tt.want {
				t.Errorf("Check(%s) = %v, %v, want %v, nil", tt.name, revoked, err, tt.want)
			}
		}
	})

	t.Run("another user is not affected", func(t *testing.T) {
		s, uid := newStore(), uuid.NewV7()
		if err := s.RevokeUser(ctx, uid, at); err != nil {
			t.Fatalf("RevokeUser() = %v", err)
		}
		if revoked, err := s.Check(ctx, uuid.NewV7(), uuid.NewV7(), at.Add(-time.Hour)); err != nil || revoked {
			t.Errorf("Check(other user) = %v, %v, want false, nil", revoked, err)
		}
	})
}
