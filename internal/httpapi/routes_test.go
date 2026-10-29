package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/logtest"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
)

type panicStore struct {
	task.Store
}

func (s panicStore) Get(context.Context, uuid.UUID) (task.Task, error) {
	panic("boom")
}

type fakeLimiter struct {
	ok         bool
	retryAfter time.Duration
	keys       []string
}

func (f *fakeLimiter) Allow(key string) (bool, time.Duration) {
	f.keys = append(f.keys, key)
	return f.ok, f.retryAfter
}

func setupTestWithLimiter(t *testing.T, store task.Store, l Limiter) http.Handler {
	t.Helper()
	logger := slog.New(NewRequestIDContextHandler(logtest.NewRecorder()))
	return NewHandler(store, logger, nil, l)
}

func TestHandler(t *testing.T) {

	t.Run("non-exist path", func(t *testing.T) {
		h, _, _ := setupTest(t)
		rec, req := prepareTestRequest(t, http.MethodGet, "/abc", nil)
		h.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusNotFound)
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		h, _, _ := setupTest(t)
		rec, req := prepareTestRequest(t, http.MethodDelete, "/tasks", nil)
		h.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusMethodNotAllowed)
		}
	})

	t.Run("X-Request-ID header returned and logged", func(t *testing.T) {
		h, _, logRec := setupTest(t)
		reqIDHeader := "X-Request-ID"
		wantReqID := "abc"
		rec, req := prepareTestRequest(t, http.MethodDelete, "/tasks", nil)
		req.Header.Set(reqIDHeader, wantReqID)
		h.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		got := res.Header.Get(reqIDHeader)
		if got == "" {
			t.Fatalf("%s header is missing", reqIDHeader)
		}

		if got != wantReqID {
			t.Fatalf("%s = %q, want %q", reqIDHeader, got, wantReqID)
		}

		logtest.CheckEntries(t, logRec.Entries(), []logtest.Entry{{
			Level: slog.LevelInfo,
			Msg:   "request completed",
			Attrs: map[string]any{"request_id": wantReqID},
		}}, logtest.OnlyAttrs("request_id"))
	})

	t.Run("panic recovered", func(t *testing.T) {
		store := panicStore{Store: memory.NewTaskStore()}
		h, logRec := setupTestWithStore(t, store)
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks/018f0000-0000-7000-8000-000000000000", nil)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}

		res := rec.Result()
		defer res.Body.Close()

		errorMsg := getErrorMessageFromResponseBody(t, res.Body)
		if !strings.Contains(errorMsg, "internal server error") {
			t.Fatalf(`body = %q, want generic message "internal server error"`, errorMsg)
		}

		if strings.Contains(errorMsg, "boom") {
			t.Fatalf("body leaks panic value: %q", errorMsg)
		}

		logtest.CheckEntries(
			t,
			logRec.Entries(),
			[]logtest.Entry{
				{
					Level: slog.LevelError,
					Msg:   "panic recovered",
					Attrs: map[string]any{"panic": "boom"},
				},
				{
					Level: slog.LevelError,
					Msg:   "request completed",
					Attrs: map[string]any{"status": int64(500)},
				},
			},
			logtest.IgnoreAttrs("duration_ms", "stack", "method", "path", "request_id"),
		)
	})

	t.Run("rate limited", func(t *testing.T) {
		// panicStore turns any request that reaches the handler into a 500.
		l := &fakeLimiter{ok: false, retryAfter: 1500 * time.Millisecond}
		h := setupTestWithLimiter(t, panicStore{Store: memory.NewTaskStore()}, l)
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks/018f0000-0000-7000-8000-000000000000", nil)
		h.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusTooManyRequests)
		}

		if got := res.Header.Get("Retry-After"); got != "2" {
			t.Errorf("Retry-After = %q, want %q", got, "2")
		}

		if got := getErrorMessageFromResponseBody(t, res.Body); got != "too many requests" {
			t.Errorf("error message = %q, want %q", got, "too many requests")
		}
	})

	t.Run("rate limited request keeps its route in the metrics", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		logger := slog.New(NewRequestIDContextHandler(logtest.NewRecorder()))
		h := NewHandler(memory.NewTaskStore(), logger, NewMetrics(reg), &fakeLimiter{ok: false})

		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks/018f0000-0000-7000-8000-000000000000", nil)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}

		families, err := reg.Gather()
		if err != nil {
			t.Fatalf("gather metrics: %v", err)
		}

		for _, f := range families {
			if f.GetName() != "http_requests_total" {
				continue
			}

			for _, m := range f.GetMetric() {
				labels := map[string]string{}
				for _, l := range m.GetLabel() {
					labels[l.GetName()] = l.GetValue()
				}

				if labels["status"] == "429" && labels["route"] != "/tasks/{id}" {
					t.Errorf("429 counted with route %q, want %q", labels["route"], "/tasks/{id}")
				}
				return
			}
		}
		t.Error("http_requests_total has no sample")
	})

	t.Run("not rate limited", func(t *testing.T) {
		l := &fakeLimiter{ok: true}
		h := setupTestWithLimiter(t, memory.NewTaskStore(), l)
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks", nil)
		req.RemoteAddr = "192.0.2.7:4321"
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		if got := rec.Header().Get("Retry-After"); got != "" {
			t.Errorf("Retry-After = %q, want it unset", got)
		}

		if len(l.keys) != 1 || l.keys[0] != "ip:192.0.2.7" {
			t.Errorf("limiter keys = %q, want [%q]", l.keys, "ip:192.0.2.7")
		}
	})

	t.Run("request without a client IP is not limited", func(t *testing.T) {
		l := &fakeLimiter{ok: false}
		h := setupTestWithLimiter(t, memory.NewTaskStore(), l)
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks", nil)
		req.RemoteAddr = "@"
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		if len(l.keys) != 0 {
			t.Errorf("limiter was called with %q, want no call", l.keys)
		}
	})
}
