package worker

import (
	"context"
	"errors"
	"log/slog"
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
		name       string
		script     []result
		wantRecord logtest.Want
	}{
		{"marks some", []result{{marked: 3}}, logtest.Want{Level: slog.LevelInfo, Msg: "overdue scan completed", Attrs: map[string]any{"count_updated": int64(3)}}},
		{"marks none", []result{{marked: 0}}, logtest.Want{Level: slog.LevelDebug, Msg: "overdue scan completed", Attrs: map[string]any{"count_updated": int64(0)}}},
		{"store error", []result{{err: errBoom}}, logtest.Want{Level: slog.LevelError, Msg: "mark overdue failed", Attrs: map[string]any{"error": errBoom}}},
		{"store panic", []result{{boom: "boom"}}, logtest.Want{Level: slog.LevelError, Msg: "panic recovered", Attrs: map[string]any{"panic": "boom"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l, rec := logtest.New()
			s := &mockStore{script: tt.script}

			scanOverdue(t.Context(), s, l, testTick)

			if len(s.calls) != 1 {
				t.Fatalf("MarkOverdue calls = %d, want 1", len(s.calls))
			}

			logtest.CheckLogRecords(t, rec.Records(), tt.wantRecord)
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
		wantRecord := logtest.Want{
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

		gotRecs := rec.Find(wantRecord.Msg)
		if len(gotRecs) != 1 {
			t.Fatalf("%q log records count = %d, want 1", wantRecord.Msg, len(gotRecs))
		}

		logtest.CheckLogRecord(t, gotRecs[0], wantRecord)
	})

	t.Run("context closed", func(t *testing.T) {
		l, rec := logtest.New()
		s := &mockStore{called: make(chan struct{}, 16)}
		ctx, cancel := context.WithCancel(t.Context())
		wg := &sync.WaitGroup{}

		wantRecord := logtest.Want{
			Level: slog.LevelInfo,
			Msg:   "worker stopped",
		}

		wg.Go(func() { Run(ctx, s, l, 5*time.Millisecond) })

		cancel()
		wg.Wait()

		gotRecs := rec.Find(wantRecord.Msg)
		if len(gotRecs) != 1 {
			t.Fatalf("%q log records count = %d, want 1", wantRecord.Msg, len(gotRecs))
		}

		logtest.CheckLogRecord(t, gotRecs[0], wantRecord)

	})

	t.Run("panic recovered", func(t *testing.T) {
		l, rec := logtest.New()
		s := &mockStore{
			script: []result{{boom: "boom"}, {marked: 3}},
			called: make(chan struct{}, 16),
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		wantRecoveredRec := logtest.Want{
			Level: slog.LevelError,
			Msg:   "panic recovered",
			Attrs: map[string]any{"panic": "boom"},
		}
		wantCompletedRec := logtest.Want{
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

		recoveredRecs := rec.Find(wantRecoveredRec.Msg)
		if len(recoveredRecs) != 1 {
			t.Fatalf("%q log records count = %d, want 1", wantRecoveredRec.Msg, len(recoveredRecs))
		}
		logtest.CheckLogRecord(t, recoveredRecs[0], wantRecoveredRec)

		completedRecs := rec.Find(wantCompletedRec.Msg)
		if len(completedRecs) != 1 {
			t.Fatalf("%q log records count = %d, want 1", wantCompletedRec.Msg, len(completedRecs))
		}
		logtest.CheckLogRecord(t, completedRecs[0], wantCompletedRec)
	})
}
