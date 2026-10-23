// Package logtest provides an in-memory [slog.Handler] for asserting on log
// records in tests.
package logtest

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
)

type Recorder struct {
	mu      *sync.Mutex
	records *[]slog.Record
	attrs   []slog.Attr
}

func NewRecorder() *Recorder {
	rec := &Recorder{
		mu:      &sync.Mutex{},
		records: new([]slog.Record),
	}
	return rec
}

func New() (*slog.Logger, *Recorder) {
	h := NewRecorder()
	return slog.New(h), h
}

func (r *Recorder) Enabled(context.Context, slog.Level) bool {
	return true // debug logs are recorded as well
}

func (r *Recorder) Handle(_ context.Context, rec slog.Record) error {
	rec = rec.Clone()
	rec.AddAttrs(r.attrs...)
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.records = append(*r.records, rec)
	return nil
}

func (r *Recorder) WithAttrs(a []slog.Attr) slog.Handler {
	c := *r
	c.attrs = append(slices.Clone(r.attrs), a...)
	return &c
}

func (r *Recorder) WithGroup(string) slog.Handler {
	panic("WithGroup is not supported")
}

func (r *Recorder) Records() []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(*r.records)
}

func (r *Recorder) Find(msg string) []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]slog.Record, 0)
	for _, rec := range *r.records {
		if rec.Message == msg {
			out = append(out, rec)
		}
	}

	return out
}

func Attrs(rec slog.Record) map[string]slog.Value {
	m := map[string]slog.Value{}
	rec.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

func MustAttr[T any](t *testing.T, attrs map[string]slog.Value, key string) T {
	t.Helper()
	v, ok := attrs[key]
	if !ok {
		t.Fatalf("missing attr %q", key)
	}

	got, ok := v.Any().(T)
	if !ok {
		var zero T
		t.Fatalf("attr %q is %T(%v), want type %T", key, v.Any(), v.Any(), zero)
	}
	return got
}

type Want struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

func CheckLogRecord(tb testing.TB, got slog.Record, want Want) {
	tb.Helper()

	if got.Level != want.Level {
		tb.Errorf("level = %v, want %v", got.Level, want.Level)
	}

	if got.Message != want.Msg {
		tb.Errorf("message = %q, want %q", got.Message, want.Msg)
	}

	attrs := Attrs(got)
	for k, want := range want.Attrs {
		v, ok := attrs[k]
		if !ok {
			tb.Errorf("missing attribute %q", k)
			continue
		}

		switch w := want.(type) {
		case error:
			got, _ := v.Any().(error)
			if !errors.Is(got, w) {
				tb.Errorf("attribute %q = error %q, want error matching %q", k, got, w)
			}
		default:
			if val := v.Any(); val != want {
				tb.Errorf("attribute %q = %T(%v), want %T(%v)", k, val, val, want, want)
			}
		}
	}
}

func CheckLogRecords(tb testing.TB, got []slog.Record, want ...Want) {
	tb.Helper()

	if len(got) != len(want) {
		tb.Fatalf("log records count = %d, want %d", len(got), len(want))
	}

	for i := range got {
		CheckLogRecord(tb, got[i], want[i])
	}
}
