package worker

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dung204/taskapi/internal/logtest"
	"github.com/dung204/taskapi/internal/task"
)

type result struct {
	marked int64
	err    error
	boom   any // for panic, nil means no panic
}

type mockStore struct {
	task.Store
	script []result      // for 1st, 2nd, 3rd, ... call
	called chan struct{} // buffered channel, nil if not needed

	mu    sync.Mutex
	calls []time.Time
}

func (s *mockStore) MarkOverdue(_ context.Context, now time.Time) (int64, error) {
	s.mu.Lock()
	s.calls = append(s.calls, now)
	var r result
	if n := len(s.calls); n <= len(s.script) {
		r = s.script[n-1]
	}
	s.mu.Unlock()

	if s.called != nil {
		select {
		case s.called <- struct{}{}: // try to send signal into channel
		default: // if can not send, do nothing
		}
	}

	if r.boom != nil {
		panic(r.boom)
	}
	return r.marked, r.err
}

var testTick = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestScanOverdue(t *testing.T) {
	errBoom := errors.New("boom")
	tests := []struct {
		name   string
		script []result
		want   logtest.Entry
	}{
		{"marks some", []result{{marked: 3}}, logtest.Entry{Level: slog.LevelInfo, Msg: "overdue scan completed", Attrs: map[string]any{"count_updated": int64(3)}}},
		{"marks none", []result{{marked: 0}}, logtest.Entry{Level: slog.LevelDebug, Msg: "overdue scan completed", Attrs: map[string]any{"count_updated": int64(0)}}},
		{"store error", []result{{err: errBoom}}, logtest.Entry{Level: slog.LevelError, Msg: "mark overdue failed", Attrs: map[string]any{"error": errBoom}}},
		{"store panic", []result{{boom: "boom"}}, logtest.Entry{Level: slog.LevelError, Msg: "panic recovered", Attrs: map[string]any{"panic": "boom"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, rec := logtest.New()
			s := &mockStore{script: tt.script}

			scanOverdue(t.Context(), s, l, testTick)

			if len(s.calls) != 1 {
				t.Fatalf("MarkOverdue calls = %d, want 1", len(s.calls))
			}

			logtest.CheckEntries(t, rec.Entries(), []logtest.Entry{tt.want}, logtest.IgnoreAttrs("stack", "duration_ms"))
		})
	}

	t.Run("passes tick time to store", func(t *testing.T) {
		l, _ := logtest.New()
		s := &mockStore{}

		scanOverdue(t.Context(), s, l, testTick)

		if len(s.calls) != 1 {
			t.Fatalf("MarkOverdue calls = %d, want 1", len(s.calls))
		}

		if !s.calls[0].Equal(testTick) {
			t.Errorf("MarkOverdue now = %v, want %v", s.calls[0], testTick)
		}
	})
}

func waitCalls(t *testing.T, called <-chan struct{}, n int) {
	t.Helper()
	for range n {
		select {
		case <-called:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for MarkOverdue")
		}
	}
}

func TestRun(t *testing.T) {
	t.Run("started once", func(t *testing.T) {
		l, rec := logtest.New()
		s := &mockStore{called: make(chan struct{}, 16)}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		wantIntervalValue := 5 * time.Millisecond
		want := logtest.Entry{
			Level: slog.LevelInfo,
			Msg:   "worker started",
			Attrs: map[string]any{"interval": wantIntervalValue.String()},
		}

		go func() {
			defer close(done)
			Run(ctx, s, l, wantIntervalValue)
		}()

		waitCalls(t, s.called, 3)
		cancel()
		<-done // wait for close(done) to finish then read the logs

		got := rec.EntriesByMsg(want.Msg)
		if len(got) != 1 {
			t.Fatalf("%q log records count = %d, want 1", want.Msg, len(got))
		}

		logtest.CheckEntry(t, got[0], want)
	})

	t.Run("context closed", func(t *testing.T) {
		l, rec := logtest.New()
		s := &mockStore{called: make(chan struct{}, 16)}
		ctx, cancel := context.WithCancel(t.Context())
		wg := &sync.WaitGroup{}

		want := logtest.Entry{
			Level: slog.LevelInfo,
			Msg:   "worker stopped",
		}

		wg.Go(func() { Run(ctx, s, l, 5*time.Millisecond) })

		cancel()
		wg.Wait()

		got := rec.EntriesByMsg(want.Msg)
		if len(got) != 1 {
			t.Fatalf("%q log records count = %d, want 1", want.Msg, len(got))
		}

		logtest.CheckEntry(t, got[0], want)

	})

	t.Run("panic recovered", func(t *testing.T) {
		l, rec := logtest.New()
		s := &mockStore{
			script: []result{{boom: "boom"}, {marked: 3}},
			called: make(chan struct{}, 16),
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		wantRecovered := logtest.Entry{
			Level: slog.LevelError,
			Msg:   "panic recovered",
			Attrs: map[string]any{"panic": "boom"},
		}
		wantCompleted := logtest.Entry{
			Level: slog.LevelInfo,
			Msg:   "overdue scan completed",
			Attrs: map[string]any{"count_updated": int64(3)},
		}

		go func() {
			defer close(done)
			Run(ctx, s, l, 5*time.Millisecond)
		}()

		waitCalls(t, s.called, len(s.script))
		cancel()
		<-done

		gotRecovered := rec.EntriesByMsg(wantRecovered.Msg)
		if len(gotRecovered) != 1 {
			t.Fatalf("%q log records count = %d, want 1", wantRecovered.Msg, len(gotRecovered))
		}
		logtest.CheckEntry(t, gotRecovered[0], wantRecovered, logtest.IgnoreAttrs("stack"))

		// Later ticks may also log "overdue scan completed" (with count_updated = 0)
		// before cancel() lands, so only look at the scan that marked 3 tasks.
		gotCompleted := slices.DeleteFunc(rec.EntriesByMsg(wantCompleted.Msg), func(e logtest.Entry) bool {
			return !e.HasAttr("count_updated", int64(3))
		})
		if len(gotCompleted) != 1 {
			t.Fatalf("%q log records with count_updated = 3 count = %d, want 1", wantCompleted.Msg, len(gotCompleted))
		}
		logtest.CheckEntry(t, gotCompleted[0], wantCompleted, logtest.IgnoreAttrs("duration_ms"))
	})
}
