package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

type fakeClock struct {
	t time.Time
}

func (c *fakeClock) now() time.Time { return c.t }

func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter(limit rate.Limit, burst int) (*MemoryLimiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	l := NewMemoryLimiter(limit, burst)
	l.now = clock.now
	return l, clock
}

func TestMemoryLimiterAllow(t *testing.T) {
	// 1 request per second, burst of 2.
	steps := []struct {
		name      string
		advance   time.Duration
		wantOK    bool
		wantRetry time.Duration
	}{
		{name: "first uses the burst", wantOK: true},
		{name: "second uses the burst", wantOK: true},
		{name: "burst exhausted", wantRetry: time.Second},
		{name: "a refused request does not consume a token", wantRetry: time.Second},
		{name: "half a token refilled", advance: 500 * time.Millisecond, wantRetry: 500 * time.Millisecond},
		{name: "one token refilled", advance: 500 * time.Millisecond, wantOK: true},
		{name: "refilled token spent", wantRetry: time.Second},
		{name: "refill is capped at the burst (1)", advance: time.Minute, wantOK: true},
		{name: "refill is capped at the burst (2)", wantOK: true},
		{name: "refill is capped at the burst (3)", wantRetry: time.Second},
	}

	l, clock := newTestLimiter(1, 2)
	for _, s := range steps {
		clock.advance(s.advance)

		gotOK, gotRetry := l.Allow("a")
		if gotOK != s.wantOK || gotRetry != s.wantRetry {
			t.Fatalf("%s: Allow() = (%t, %v), want (%t, %v)", s.name, gotOK, gotRetry, s.wantOK, s.wantRetry)
		}
	}
}

func TestMemoryLimiterKeysAreIndependent(t *testing.T) {
	l, _ := newTestLimiter(1, 1)

	if ok, _ := l.Allow("a"); !ok {
		t.Fatal(`Allow("a") = false, want true`)
	}

	if ok, _ := l.Allow("a"); ok {
		t.Fatal(`second Allow("a") = true, want false`)
	}

	if ok, _ := l.Allow("b"); !ok {
		t.Error(`Allow("b") = false, want true: another key's usage must not count`)
	}

	if got := l.Len(); got != 2 {
		t.Errorf("Len() = %d, want 2", got)
	}
}

func TestMemoryLimiterConcurrent(t *testing.T) {
	const burst = 10

	// The clock is frozen and the refill is slow, so exactly burst calls may succeed.
	l, _ := newTestLimiter(rate.Every(time.Hour), burst)

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			if ok, _ := l.Allow("a"); ok {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()

	if got := allowed.Load(); got != burst {
		t.Errorf("allowed = %d, want %d", got, burst)
	}
}

func TestMemoryLimiterCleanup(t *testing.T) {
	l, clock := newTestLimiter(1, 1)

	l.Allow("old")
	clock.advance(l.ttl)
	l.Allow("mid")
	clock.advance(time.Nanosecond)
	l.Allow("fresh")

	// "old" has been idle for ttl+1ns and goes; the others were seen more recently.
	l.cleanup(clock.now())
	if got := l.Len(); got != 2 {
		t.Fatalf("Len() after cleanup = %d, want 2", got)
	}

	// "mid" has now been idle for ttl+1ns and goes; "fresh" for exactly ttl and stays.
	clock.advance(l.ttl)
	l.cleanup(clock.now())
	if got := l.Len(); got != 1 {
		t.Fatalf("Len() after second cleanup = %d, want 1", got)
	}

	// An evicted key starts over with a full burst.
	if ok, _ := l.Allow("old"); !ok {
		t.Error(`Allow("old") after eviction = false, want true`)
	}
}

func TestMemoryLimiterRunStopsOnCancel(t *testing.T) {
	l, _ := newTestLimiter(1, 1)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		l.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}
}
