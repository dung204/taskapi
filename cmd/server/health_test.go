package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dung204/taskapi/internal/testutil/logtest"
)

var errBoom = errors.New("boom")

func okPing(context.Context) error   { return nil }
func failPing(context.Context) error { return errBoom }

func checkHealthResponse(tb testing.TB, got, want healthResponse, opts ...cmp.Option) {
	tb.Helper()
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("health response mismatch (-want +got):\n%s", diff)
	}
}

func TestSummarize(t *testing.T) {
	tests := []struct {
		name     string
		checks   []*check
		errs     []error
		wantCode int
		want     healthResponse
	}{
		{
			"no checks", nil, nil,
			http.StatusOK,
			healthResponse{Status: HealthStatusOK, Checks: map[string]string{}},
		},
		{
			"all ok",
			[]*check{newCheck("postgres", true, okPing), newCheck("redis", false, okPing)},
			[]error{nil, nil},
			http.StatusOK,
			healthResponse{Status: HealthStatusOK, Checks: map[string]string{"postgres": "ok", "redis": "ok"}},
		},
		{
			"non-critical failure degrades but stays 200",
			[]*check{newCheck("postgres", true, okPing), newCheck("redis", false, okPing)},
			[]error{nil, errBoom},
			http.StatusOK,
			healthResponse{Status: HealthStatusDegraded, Checks: map[string]string{"postgres": "ok", "redis": "unavailable"}},
		},
		{
			"critical failure is 503",
			[]*check{newCheck("postgres", true, okPing), newCheck("redis", false, okPing)},
			[]error{errBoom, nil},
			http.StatusServiceUnavailable,
			healthResponse{Status: HealthStatusUnavailable, Checks: map[string]string{"postgres": "unavailable", "redis": "ok"}},
		},
		{
			"critical failure wins over an earlier non-critical one",
			[]*check{newCheck("redis", false, okPing), newCheck("postgres", true, okPing)},
			[]error{errBoom, errBoom},
			http.StatusServiceUnavailable,
			healthResponse{Status: HealthStatusUnavailable, Checks: map[string]string{"postgres": "unavailable", "redis": "unavailable"}},
		},
		{
			"critical failure wins over a later non-critical one",
			[]*check{newCheck("postgres", true, okPing), newCheck("redis", false, okPing)},
			[]error{errBoom, errBoom},
			http.StatusServiceUnavailable,
			healthResponse{Status: HealthStatusUnavailable, Checks: map[string]string{"postgres": "unavailable", "redis": "unavailable"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, code := summarize(tt.checks, tt.errs)
			if code != tt.wantCode {
				t.Errorf("status code = %d, want %d", code, tt.wantCode)
			}
			checkHealthResponse(t, got, tt.want)
		})
	}
}

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name           string
		checks         []*check
		wantStatusCode int
		want           healthResponse
	}{
		{
			"no checks (memory store)", nil,
			http.StatusOK,
			healthResponse{Status: HealthStatusOK, Checks: map[string]string{}},
		},
		{
			"all ok",
			[]*check{newCheck("postgres", true, okPing), newCheck("redis", false, okPing)},
			http.StatusOK,
			healthResponse{Status: HealthStatusOK, Checks: map[string]string{"postgres": "ok", "redis": "ok"}},
		},
		{
			"redis down is degraded",
			[]*check{newCheck("postgres", true, okPing), newCheck("redis", false, failPing)},
			http.StatusOK,
			healthResponse{Status: HealthStatusDegraded, Checks: map[string]string{"postgres": "ok", "redis": "unavailable"}},
		},
		{
			"postgres down is unavailable",
			[]*check{newCheck("postgres", true, failPing), newCheck("redis", false, okPing)},
			http.StatusServiceUnavailable,
			healthResponse{Status: HealthStatusUnavailable, Checks: map[string]string{"postgres": "unavailable", "redis": "ok"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, _ := logtest.New()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()
			healthHandler(logger, tt.checks...).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Fatalf("status code = %v, want %v", rec.Code, tt.wantStatusCode)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			if bytes.Contains(rec.Body.Bytes(), []byte(errBoom.Error())) {
				t.Errorf("body leaks the error text: %s", rec.Body)
			}

			var got healthResponse
			dec := json.NewDecoder(rec.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			checkHealthResponse(t, got, tt.want)
		})
	}
}

func TestCheckRunLogsTransitions(t *testing.T) {
	degraded := logtest.Entry{Level: slog.LevelWarn, Msg: "health check degraded", Attrs: map[string]any{"check": "redis"}}
	recovered := logtest.Entry{Level: slog.LevelInfo, Msg: "health check recovered", Attrs: map[string]any{"check": "redis"}}

	tests := []struct {
		name  string
		steps []error // result of each successive probe
		want  []logtest.Entry
	}{
		{"healthy stays quiet", []error{nil, nil, nil}, nil},
		{"failure then recovery", []error{errBoom, nil}, []logtest.Entry{degraded, recovered}},
		{"repeated failure logs once", []error{errBoom, errBoom, errBoom}, []logtest.Entry{degraded}},
		{"flapping logs every transition", []error{errBoom, nil, errBoom}, []logtest.Entry{degraded, recovered, degraded}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pingErr error
			c := newCheck("redis", false, func(context.Context) error { return pingErr })
			logger, rec := logtest.New()

			for i, step := range tt.steps {
				pingErr = step
				if got := c.run(t.Context(), logger); !errors.Is(got, step) {
					t.Fatalf("step %d: run() = %v, want %v", i, got, step)
				}
			}

			logtest.CheckEntries(t, rec.Entries(), tt.want, cmpopts.EquateEmpty())
		})
	}
}

func TestRunChecks(t *testing.T) {
	t.Run("results keep the order of the checks", func(t *testing.T) {
		logger, _ := logtest.New()
		checks := []*check{
			newCheck("a", true, failPing),
			newCheck("b", true, okPing),
			newCheck("c", true, failPing),
		}

		errs := runChecks(t.Context(), logger, checks)

		got := []bool{errs[0] != nil, errs[1] != nil, errs[2] != nil}
		if diff := cmp.Diff([]bool{true, false, true}, got); diff != "" {
			t.Errorf("failed checks mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("checks run concurrently", func(t *testing.T) {
		// Each check waits for the other to start. Run one after the other,
		// the first would block until the deadline and fail.
		logger, _ := logtest.New()
		aStarted, bStarted := make(chan struct{}), make(chan struct{})
		rendezvous := func(mine, other chan struct{}) func(context.Context) error {
			return func(ctx context.Context) error {
				close(mine)
				select {
				case <-other:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
		checks := []*check{
			newCheck("a", true, rendezvous(aStarted, bStarted)),
			newCheck("b", true, rendezvous(bStarted, aStarted)),
		}

		for i, err := range runChecks(t.Context(), logger, checks) {
			if err != nil {
				t.Errorf("check %d: %v (not run concurrently?)", i, err)
			}
		}
	})

	t.Run("a hung check is cut off by the deadline", func(t *testing.T) {
		logger, _ := logtest.New()
		hang := func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}
		checks := []*check{newCheck("hung", true, hang), newCheck("fine", true, okPing)}

		// A shorter parent deadline keeps the test fast; runChecks must
		// honour the earlier of the two.
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		defer cancel()

		errs := runChecks(ctx, logger, checks)

		if !errors.Is(errs[0], context.DeadlineExceeded) {
			t.Errorf("hung check error = %v, want context.DeadlineExceeded", errs[0])
		}
		if errs[1] != nil {
			t.Errorf("fine check error = %v, want nil", errs[1])
		}
	})
}
