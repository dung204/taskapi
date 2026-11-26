package auth

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/testutil/logtest"
)

// limiterFailOpen returns auth_limiter_fail_open_total{limiter=name} from reg,
// or 0 if the series does not exist yet.
func limiterFailOpen(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() = %v", err)
	}
	for _, f := range families {
		if f.GetName() != "auth_limiter_fail_open_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "limiter" && l.GetValue() == name {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

type fakeLoginLimiter struct {
	wait     time.Duration
	err      error
	attempts []string
	resets   []string
}

func (f *fakeLoginLimiter) Attempt(_ context.Context, email string) (time.Duration, error) {
	f.attempts = append(f.attempts, email)
	return f.wait, f.err
}

func (f *fakeLoginLimiter) Reset(_ context.Context, email string) error {
	f.resets = append(f.resets, email)
	return f.err
}

type fakeMailRequestLimiter struct {
	retryAfter time.Duration
	err        error
	calls      []string
}

func (f *fakeMailRequestLimiter) Allow(_ context.Context, email string) (time.Duration, error) {
	f.calls = append(f.calls, email)
	return f.retryAfter, f.err
}

func TestFailOpenLoginLimiter(t *testing.T) {
	ctx := t.Context()
	const addr = "alice@example.com"
	errBackend := errors.New("redis down")

	t.Run("passes the answer through and stays quiet on success", func(t *testing.T) {
		next := &fakeLoginLimiter{wait: 7 * time.Second}
		logger, rec := logtest.New()
		reg := prometheus.NewRegistry()
		l := NewFailOpenLoginLimiter(next, logger, metrics.NewAuth(reg))

		wait, err := l.Attempt(ctx, addr)
		if err != nil || wait != 7*time.Second {
			t.Errorf("Attempt() = %v, %v, want 7s, nil", wait, err)
		}
		if err := l.Reset(ctx, addr); err != nil {
			t.Errorf("Reset() = %v, want nil", err)
		}

		if len(next.attempts) != 1 || next.attempts[0] != addr {
			t.Errorf("backend attempts = %v, want [%s]", next.attempts, addr)
		}
		if len(next.resets) != 1 || next.resets[0] != addr {
			t.Errorf("backend resets = %v, want [%s]", next.resets, addr)
		}
		if got := rec.Entries(); len(got) != 0 {
			t.Errorf("logged %v on success, want nothing", got)
		}
		if got := limiterFailOpen(t, reg, metrics.LimiterLogin); got != 0 {
			t.Errorf("fail_open{limiter=login} = %v on success, want 0", got)
		}
	})

	t.Run("a backend error lets the attempt through, with no wait", func(t *testing.T) {
		// Even if the backend returns a wait together with the error, the
		// caller must not be told to wait: the limiter failed open.
		next := &fakeLoginLimiter{wait: time.Minute, err: errBackend}
		logger, rec := logtest.New()
		reg := prometheus.NewRegistry()
		l := NewFailOpenLoginLimiter(next, logger, metrics.NewAuth(reg))

		wait, err := l.Attempt(ctx, addr)
		if err != nil || wait != 0 {
			t.Errorf("Attempt() = %v, %v, want 0, nil", wait, err)
		}

		got := rec.Entries()
		if len(got) != 1 || got[0].Level != slog.LevelWarn || !got[0].HasAttr("error", errBackend) {
			t.Errorf("log entries = %v, want one warning with the error", got)
		}
		if got := limiterFailOpen(t, reg, metrics.LimiterLogin); got != 1 {
			t.Errorf("fail_open{limiter=login} = %v, want 1", got)
		}
	})

	t.Run("a failing Reset is swallowed and counted for the login limiter", func(t *testing.T) {
		next := &fakeLoginLimiter{err: errBackend}
		logger, rec := logtest.New()
		reg := prometheus.NewRegistry()
		l := NewFailOpenLoginLimiter(next, logger, metrics.NewAuth(reg))

		if err := l.Reset(ctx, addr); err != nil {
			t.Errorf("Reset() = %v, want nil", err)
		}

		got := rec.Entries()
		if len(got) != 1 || got[0].Level != slog.LevelWarn || !got[0].HasAttr("error", errBackend) {
			t.Errorf("log entries = %v, want one warning with the error", got)
		}
		if got := limiterFailOpen(t, reg, metrics.LimiterLogin); got != 1 {
			t.Errorf("fail_open{limiter=login} = %v, want 1", got)
		}
		if got := limiterFailOpen(t, reg, metrics.LimiterMail); got != 0 {
			t.Errorf("fail_open{limiter=mail} = %v, want 0: a login limiter must not count as the mail one", got)
		}
	})

	t.Run("works without metrics or a logger", func(t *testing.T) {
		l := NewFailOpenLoginLimiter(&fakeLoginLimiter{err: errBackend}, nil, nil)

		if wait, err := l.Attempt(ctx, addr); err != nil || wait != 0 {
			t.Errorf("Attempt() = %v, %v, want 0, nil", wait, err)
		}
		if err := l.Reset(ctx, addr); err != nil {
			t.Errorf("Reset() = %v, want nil", err)
		}
	})
}

func TestFailOpenMailRequestLimiter(t *testing.T) {
	ctx := t.Context()
	const addr = "alice@example.com"
	errBackend := errors.New("redis down")

	t.Run("passes the answer through and stays quiet on success", func(t *testing.T) {
		next := &fakeMailRequestLimiter{retryAfter: 30 * time.Second}
		logger, rec := logtest.New()
		reg := prometheus.NewRegistry()
		l := NewFailOpenAuthMailLimiter(next, logger, metrics.NewAuth(reg))

		retryAfter, err := l.Allow(ctx, addr)
		if err != nil || retryAfter != 30*time.Second {
			t.Errorf("Allow() = %v, %v, want 30s, nil", retryAfter, err)
		}
		if len(next.calls) != 1 || next.calls[0] != addr {
			t.Errorf("backend calls = %v, want [%s]", next.calls, addr)
		}
		if got := rec.Entries(); len(got) != 0 {
			t.Errorf("logged %v on success, want nothing", got)
		}
		if got := limiterFailOpen(t, reg, metrics.LimiterMail); got != 0 {
			t.Errorf("fail_open{limiter=mail} = %v on success, want 0", got)
		}
	})

	t.Run("a backend error allows the request, with no wait", func(t *testing.T) {
		next := &fakeMailRequestLimiter{retryAfter: time.Minute, err: errBackend}
		logger, rec := logtest.New()
		reg := prometheus.NewRegistry()
		l := NewFailOpenAuthMailLimiter(next, logger, metrics.NewAuth(reg))

		retryAfter, err := l.Allow(ctx, addr)
		if err != nil || retryAfter != 0 {
			t.Errorf("Allow() = %v, %v, want 0, nil", retryAfter, err)
		}

		got := rec.Entries()
		if len(got) != 1 || got[0].Level != slog.LevelWarn || !got[0].HasAttr("error", errBackend) {
			t.Errorf("log entries = %v, want one warning with the error", got)
		}
		if got := limiterFailOpen(t, reg, metrics.LimiterMail); got != 1 {
			t.Errorf("fail_open{limiter=mail} = %v, want 1", got)
		}
	})

	t.Run("works without metrics or a logger", func(t *testing.T) {
		l := NewFailOpenAuthMailLimiter(&fakeMailRequestLimiter{err: errBackend}, nil, nil)

		if retryAfter, err := l.Allow(ctx, addr); err != nil || retryAfter != 0 {
			t.Errorf("Allow() = %v, %v, want 0, nil", retryAfter, err)
		}
	})
}
