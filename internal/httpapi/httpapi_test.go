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

	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil/logtest"
)

type panicStore struct {
	task.Store
}

func (s panicStore) Get(context.Context, uuid.UUID, uuid.UUID) (task.Task, error) {
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
	return withTestAuth(t, HandlerDeps{Tasks: store, Logger: logger, Limiter: l})
}

func setupTestWithCORS(t *testing.T, store task.Store, l Limiter, opts CORSMiddlewareOpts) http.Handler {
	t.Helper()
	logger := slog.New(NewRequestIDContextHandler(logtest.NewRecorder()))
	return withTestAuth(t, HandlerDeps{Tasks: store, Logger: logger, Limiter: l, CORS: opts})
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

	// The generated wrapper binds parameters before it runs the route
	// middlewares, so a bad parameter must not skip the limiter.
	t.Run("a request with a bad parameter is rate limited too", func(t *testing.T) {
		for _, path := range []string{"/tasks?limit=abc", "/tasks?offset=abc", "/tasks?status=%zz", "/tasks/not-a-uuid"} {
			t.Run(path, func(t *testing.T) {
				l := &fakeLimiter{ok: false, retryAfter: time.Second}
				h := setupTestWithLimiter(t, memory.NewTaskStore(), l)
				rec, req := prepareTestRequest(t, http.MethodGet, path, nil)
				req.RemoteAddr = "192.0.2.7:4321"
				h.ServeHTTP(rec, req)

				if rec.Code != http.StatusTooManyRequests {
					t.Fatalf("status code = %d, want %d", rec.Code, http.StatusTooManyRequests)
				}
				if got := rec.Header().Get("Retry-After"); got != "1" {
					t.Errorf("Retry-After = %q, want %q", got, "1")
				}
				if len(l.keys) != 1 || l.keys[0] != "ip:192.0.2.7" {
					t.Errorf("limiter keys = %q, want [%q]", l.keys, "ip:192.0.2.7")
				}
			})
		}
	})

	t.Run("a bad parameter within the limit is a 400 that used one token", func(t *testing.T) {
		l := &fakeLimiter{ok: true}
		h := setupTestWithLimiter(t, memory.NewTaskStore(), l)
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks?limit=abc", nil)
		req.RemoteAddr = "192.0.2.7:4321"
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusBadRequest)
		}
		if len(l.keys) != 1 {
			t.Errorf("limiter was asked %d times, want 1", len(l.keys))
		}
	})

	t.Run("rate limited request keeps its route in the metrics", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		logger := slog.New(NewRequestIDContextHandler(logtest.NewRecorder()))
		h := withTestAuth(t, HandlerDeps{
			Tasks:   memory.NewTaskStore(),
			Logger:  logger,
			Metrics: Metrics{HTTP: metrics.NewHTTP(reg)},
			Limiter: &fakeLimiter{ok: false},
		})

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

	t.Run("CORS preflight is answered before the router and the rate limiter", func(t *testing.T) {
		const origin = "https://app.example.com"
		l := &fakeLimiter{ok: false}
		h := setupTestWithCORS(t, memory.NewTaskStore(), l, CORSMiddlewareOpts{AllowedOrigins: []string{origin}, MaxAge: time.Minute})
		rec, req := prepareTestRequest(t, http.MethodOptions, "/tasks", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusNoContent)
		}

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
		}

		if got := rec.Header().Get("Access-Control-Max-Age"); got != "60" {
			t.Errorf("Access-Control-Max-Age = %q, want %q", got, "60")
		}

		if len(l.keys) != 0 {
			t.Errorf("limiter was called with %q, want no call", l.keys)
		}
	})

	t.Run("CORS headers on a regular response", func(t *testing.T) {
		const origin = "https://app.example.com"
		h := setupTestWithCORS(t, memory.NewTaskStore(), nil, CORSMiddlewareOpts{AllowedOrigins: []string{origin}})
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks", nil)
		req.Header.Set("Origin", origin)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusOK)
		}

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
		}

		if got := rec.Header().Get("Access-Control-Expose-Headers"); !strings.Contains(got, "X-Request-ID") {
			t.Errorf("Access-Control-Expose-Headers = %q, want it to contain X-Request-ID", got)
		}

		if rec.Header().Get("X-Request-ID") == "" {
			t.Error("X-Request-ID header is missing")
		}
	})

	t.Run("CORS headers on a rate limited response", func(t *testing.T) {
		// Without them a browser would hide the 429 and its Retry-After from the page.
		const origin = "https://app.example.com"
		l := &fakeLimiter{ok: false, retryAfter: time.Second}
		h := setupTestWithCORS(t, memory.NewTaskStore(), l, CORSMiddlewareOpts{AllowedOrigins: []string{origin}})
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks", nil)
		req.Header.Set("Origin", origin)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("status code = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
		}
	})

	t.Run("CORS is not wired without allowed origins", func(t *testing.T) {
		h := setupTestWithCORS(t, memory.NewTaskStore(), nil, CORSMiddlewareOpts{})

		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks", nil)
		req.Header.Set("Origin", "https://app.example.com")
		h.ServeHTTP(rec, req)

		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Access-Control-Allow-Origin = %q, want it unset", got)
		}

		rec, req = prepareTestRequest(t, http.MethodOptions, "/tasks", nil)
		req.Header.Set("Origin", "https://app.example.com")
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("preflight status code = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
	})
}

func TestUserAgentMiddleware(t *testing.T) {
	tests := []struct {
		name      string
		userAgent string
		want      string
	}{
		{name: "browser", userAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36", want: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 Chrome/126.0 Safari/537.36"},
		{name: "CLI client", userAgent: "curl/8.5.0", want: "curl/8.5.0"},
		{name: "missing header", userAgent: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls int
			var got string
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				calls++
				got = userAgentFrom(r.Context())
			})

			rec, req := prepareTestRequest(t, http.MethodGet, "/tasks", nil)
			if tt.userAgent != "" {
				req.Header.Set("User-Agent", tt.userAgent)
			}

			userAgentMiddleware(next).ServeHTTP(rec, req)

			if calls != 1 {
				t.Fatalf("next handler called %d times, want 1", calls)
			}

			if got != tt.want {
				t.Errorf("userAgentFrom() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("keeps the values already in the request context", func(t *testing.T) {
		type ctxKey struct{}

		var gotValue any
		var gotUserAgent string
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			gotValue = r.Context().Value(ctxKey{})
			gotUserAgent = userAgentFrom(r.Context())
		})

		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks", nil)
		req.Header.Set("User-Agent", "curl/8.5.0")
		req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "kept"))

		userAgentMiddleware(next).ServeHTTP(rec, req)

		if gotValue != "kept" {
			t.Errorf("context value = %v, want %q", gotValue, "kept")
		}

		if gotUserAgent != "curl/8.5.0" {
			t.Errorf("userAgentFrom() = %q, want %q", gotUserAgent, "curl/8.5.0")
		}
	})
}

func TestUserAgentFrom(t *testing.T) {
	tests := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{name: "context without the middleware", ctx: context.Background(), want: ""},
		{name: "value set by the middleware", ctx: context.WithValue(context.Background(), userAgentKey{}, "curl/8.5.0"), want: "curl/8.5.0"},
		{name: "value of another type is ignored", ctx: context.WithValue(context.Background(), userAgentKey{}, 42), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := userAgentFrom(tt.ctx); got != tt.want {
				t.Errorf("userAgentFrom() = %q, want %q", got, tt.want)
			}
		})
	}
}
