package task

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/metrics"
)

type fakeInviteLimiter struct {
	wait time.Duration
	err  error
}

func (f fakeInviteLimiter) Allow(context.Context, string) (time.Duration, error) {
	return f.wait, f.err
}

type fakeOwnerLimiter struct {
	wait time.Duration
	err  error
}

func (f fakeOwnerLimiter) Allow(context.Context, uuid.UUID) (time.Duration, error) {
	return f.wait, f.err
}

// limiterFailOpen returns task_limiter_fail_open_total{limiter=name}, or 0 if
// the series does not exist yet.
func limiterFailOpen(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() = %v", err)
	}
	for _, f := range families {
		if f.GetName() != "task_limiter_fail_open_total" {
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

func TestFailOpenInviteMailLimiter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)

	t.Run("passes the decision through", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		l := NewFailOpenInviteMailLimiter(fakeInviteLimiter{wait: time.Minute}, logger, metrics.NewTask(reg))

		got, err := l.Allow(ctx, "a@example.com")
		if err != nil || got != time.Minute {
			t.Errorf("Allow() = %v, %v, want %v, nil", got, err, time.Minute)
		}
		if n := limiterFailOpen(t, reg, metrics.LimiterInviteMail); n != 0 {
			t.Errorf("fail-open count = %v, want 0", n)
		}
	})

	t.Run("swallows and counts a backend error", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		l := NewFailOpenInviteMailLimiter(fakeInviteLimiter{wait: time.Minute, err: errors.New("redis down")}, logger, metrics.NewTask(reg))

		got, err := l.Allow(ctx, "a@example.com")
		if err != nil || got != 0 {
			t.Errorf("Allow() = %v, %v, want 0, nil", got, err)
		}
		if n := limiterFailOpen(t, reg, metrics.LimiterInviteMail); n != 1 {
			t.Errorf("fail-open count = %v, want 1", n)
		}
	})

	t.Run("works without metrics or logger", func(t *testing.T) {
		l := NewFailOpenInviteMailLimiter(fakeInviteLimiter{err: errors.New("redis down")}, nil, nil)
		if got, err := l.Allow(ctx, "a@example.com"); err != nil || got != 0 {
			t.Errorf("Allow() = %v, %v, want 0, nil", got, err)
		}
	})
}

func TestFailOpenOwnerInviteLimiter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)
	owner := uuid.NewV7()

	t.Run("passes the decision through", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		l := NewFailOpenOwnerInviteLimiter(fakeOwnerLimiter{wait: time.Minute}, logger, metrics.NewTask(reg))

		got, err := l.Allow(ctx, owner)
		if err != nil || got != time.Minute {
			t.Errorf("Allow() = %v, %v, want %v, nil", got, err, time.Minute)
		}
		if n := limiterFailOpen(t, reg, metrics.LimiterInviteOwner); n != 0 {
			t.Errorf("fail-open count = %v, want 0", n)
		}
	})

	t.Run("swallows and counts a backend error", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		l := NewFailOpenOwnerInviteLimiter(fakeOwnerLimiter{wait: time.Minute, err: errors.New("redis down")}, logger, metrics.NewTask(reg))

		got, err := l.Allow(ctx, owner)
		if err != nil || got != 0 {
			t.Errorf("Allow() = %v, %v, want 0, nil", got, err)
		}
		if n := limiterFailOpen(t, reg, metrics.LimiterInviteOwner); n != 1 {
			t.Errorf("fail-open count = %v, want 1", n)
		}
	})

	t.Run("works without metrics or logger", func(t *testing.T) {
		l := NewFailOpenOwnerInviteLimiter(fakeOwnerLimiter{err: errors.New("redis down")}, nil, nil)
		if got, err := l.Allow(ctx, owner); err != nil || got != 0 {
			t.Errorf("Allow() = %v, %v, want 0, nil", got, err)
		}
	})
}
