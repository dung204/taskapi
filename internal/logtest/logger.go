// Package logtest provides an in-memory [slog.Handler] for asserting on log
// records in tests.
package logtest

import (
	"context"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type Entry struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

func (e Entry) Attr[T any](key string) (T, bool) {
	v, ok := e.Attrs[key].(T)
	return v, ok
}

func (e Entry) HasAttr(key string, want any) bool {
	got, ok := e.Attrs[key]
	return ok && cmp.Equal(got, want, cmpopts.EquateErrors())
}

func toEntry(r slog.Record) Entry {
	var attrs map[string]any
	if r.NumAttrs() > 0 {
		attrs = make(map[string]any, r.NumAttrs())
		r.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = a.Value.Resolve().Any()
			return true
		})
	}

	return Entry{
		Level: r.Level,
		Msg:   r.Message,
		Attrs: attrs,
	}
}

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

func (r *Recorder) Entries() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()

	recs := *r.records
	entries := make([]Entry, len(recs))

	for i, rec := range recs {
		entries[i] = toEntry(rec)
	}

	return entries
}

func (r *Recorder) EntriesByMsg(msg string) []Entry {
	return slices.DeleteFunc(r.Entries(), func(e Entry) bool {
		return e.Msg != msg
	})
}

func IgnoreAttrs(keys ...string) cmp.Option {
	return cmpopts.IgnoreMapEntries(func(k string, _ any) bool {
		return slices.Contains(keys, k)
	})
}

func OnlyAttrs(keys ...string) cmp.Option {
	return cmpopts.IgnoreMapEntries(func(k string, _ any) bool {
		return !slices.Contains(keys, k)
	})
}

func CheckEntry(tb testing.TB, got, want Entry, opts ...cmp.Option) {
	tb.Helper()

	opts = append(opts, cmpopts.EquateErrors())

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("log entry mismatch (-want +got):\n%s", diff)
	}
}

func CheckEntries(tb testing.TB, got, want []Entry, opts ...cmp.Option) {
	tb.Helper()

	opts = append([]cmp.Option{cmpopts.EquateErrors()}, opts...)

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("log entries mismatch (-want +got):\n%s", diff)
	}
}

func CheckEntryPartial(tb testing.TB, got, want Entry, opts ...cmp.Option) {
	tb.Helper()

	keys := slices.Collect(maps.Keys(want.Attrs))
	opts = append(opts, OnlyAttrs(keys...))

	CheckEntry(tb, got, want, opts...)
}
