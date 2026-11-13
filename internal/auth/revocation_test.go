package auth

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/testutil/logtest"
)

// revokeFailed returns auth_revoke_failed_total{op=op} from reg, or 0 if the
// series does not exist yet.
func revokeFailed(t *testing.T, reg *prometheus.Registry, op string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() = %v", err)
	}
	for _, f := range families {
		if f.GetName() != "auth_revoke_failed_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "op" && l.GetValue() == op {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

type fakeRevoker struct {
	err      error
	sessions []uuid.UUID
	users    []uuid.UUID
}

func (f *fakeRevoker) RevokeSession(_ context.Context, sid uuid.UUID) error {
	f.sessions = append(f.sessions, sid)
	return f.err
}

func (f *fakeRevoker) RevokeUser(_ context.Context, userID uuid.UUID, _ time.Time) error {
	f.users = append(f.users, userID)
	return f.err
}

func TestFailOpenRevoker(t *testing.T) {
	ctx := t.Context()
	sid, uid := uuid.NewV7(), uuid.NewV7()
	errBackend := errors.New("redis down")

	t.Run("passes through to the backend and stays quiet on success", func(t *testing.T) {
		next := &fakeRevoker{}
		logger, rec := logtest.New()
		reg := prometheus.NewRegistry()
		r := NewFailOpenRevoker(next, logger, metrics.NewAuth(reg))

		if err := r.RevokeSession(ctx, sid); err != nil {
			t.Errorf("RevokeSession() = %v, want nil", err)
		}
		if err := r.RevokeUser(ctx, uid, testNow); err != nil {
			t.Errorf("RevokeUser() = %v, want nil", err)
		}

		if len(next.sessions) != 1 || next.sessions[0] != sid {
			t.Errorf("backend sessions = %v, want [%v]", next.sessions, sid)
		}
		if len(next.users) != 1 || next.users[0] != uid {
			t.Errorf("backend users = %v, want [%v]", next.users, uid)
		}
		if got := rec.Entries(); len(got) != 0 {
			t.Errorf("logged %v on success, want nothing", got)
		}
		for _, op := range []string{metrics.RevokeOpSession, metrics.RevokeOpUser} {
			if got := revokeFailed(t, reg, op); got != 0 {
				t.Errorf("revoke_failed{op=%q} = %v on success, want 0", op, got)
			}
		}
	})

	t.Run("swallows a backend error and logs a warning", func(t *testing.T) {
		next := &fakeRevoker{err: errBackend}
		logger, rec := logtest.New()
		reg := prometheus.NewRegistry()
		r := NewFailOpenRevoker(next, logger, metrics.NewAuth(reg))

		if err := r.RevokeSession(ctx, sid); err != nil {
			t.Errorf("RevokeSession() = %v, want nil", err)
		}
		if err := r.RevokeUser(ctx, uid, testNow); err != nil {
			t.Errorf("RevokeUser() = %v, want nil", err)
		}

		got := rec.Entries()
		if len(got) != 2 {
			t.Fatalf("logged %d entries, want 2: %v", len(got), got)
		}
		for i, e := range got {
			if e.Level != slog.LevelWarn {
				t.Errorf("entry %d level = %v, want Warn", i, e.Level)
			}
			if !e.HasAttr("error", errBackend) {
				t.Errorf("entry %d is missing the error attribute: %v", i, e.Attrs)
			}
		}
		if !got[0].HasAttr("session_id", sid) {
			t.Errorf("session entry attrs = %v, want session_id %v", got[0].Attrs, sid)
		}
		if !got[1].HasAttr("user_id", uid) {
			t.Errorf("user entry attrs = %v, want user_id %v", got[1].Attrs, uid)
		}
		for _, op := range []string{metrics.RevokeOpSession, metrics.RevokeOpUser} {
			if got := revokeFailed(t, reg, op); got != 1 {
				t.Errorf("revoke_failed{op=%q} = %v, want 1", op, got)
			}
		}
	})

	t.Run("works without metrics or a logger", func(t *testing.T) {
		r := NewFailOpenRevoker(&fakeRevoker{err: errBackend}, nil, nil)

		if err := r.RevokeSession(ctx, sid); err != nil {
			t.Errorf("RevokeSession() = %v, want nil", err)
		}
		if err := r.RevokeUser(ctx, uid, testNow); err != nil {
			t.Errorf("RevokeUser() = %v, want nil", err)
		}
	})
}
