package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func preflightRequest(tb testing.TB, origin string) *http.Request {
	tb.Helper()

	req := httptest.NewRequestWithContext(tb.Context(), http.MethodOptions, "/tasks", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	return req
}

func TestCorsMiddleware(t *testing.T) {
	const origin = "https://app.example.com"

	tests := []struct {
		name string
		opts CORSMiddlewareOpts
		req  func(tb testing.TB) *http.Request

		wantStatus    int
		wantReachNext bool
		wantHeaders   map[string]string // "" means the header must be absent
	}{
		{
			name: "preflight from an allowed origin",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}, MaxAge: 10 * time.Minute},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				return preflightRequest(tb, origin)
			},
			wantStatus: http.StatusNoContent,
			wantHeaders: map[string]string{
				"Access-Control-Allow-Origin":  origin,
				"Access-Control-Allow-Methods": "GET, POST, PATCH, DELETE",
				"Access-Control-Allow-Headers": "Content-Type, X-Request-ID, Authorization",
				"Access-Control-Max-Age":       "600",
				"Vary":                         "Origin",
			},
		},
		{
			name: "preflight with a wildcard allows any origin",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{"*"}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				return preflightRequest(tb, "https://other.example.org")
			},
			wantStatus:  http.StatusNoContent,
			wantHeaders: map[string]string{"Access-Control-Allow-Origin": "https://other.example.org", "Vary": "Origin"},
		},
		{
			name: "preflight without MaxAge omits the header",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				return preflightRequest(tb, origin)
			},
			wantStatus:  http.StatusNoContent,
			wantHeaders: map[string]string{"Access-Control-Allow-Origin": origin, "Access-Control-Max-Age": ""},
		},
		{
			name: "preflight from a disallowed origin gets no CORS headers",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}, MaxAge: time.Minute},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				return preflightRequest(tb, "https://evil.example.org")
			},
			wantStatus:  http.StatusNoContent,
			wantHeaders: map[string]string{"Access-Control-Allow-Origin": "", "Access-Control-Allow-Methods": "", "Access-Control-Max-Age": ""},
		},
		{
			name: "origin match is exact",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				return preflightRequest(tb, origin+".evil.org")
			},
			wantStatus:  http.StatusNoContent,
			wantHeaders: map[string]string{"Access-Control-Allow-Origin": ""},
		},
		{
			name: "OPTIONS without Origin is not a preflight",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				req := preflightRequest(tb, "")
				return req
			},
			wantStatus:    http.StatusTeapot,
			wantReachNext: true,
		},
		{
			name: "OPTIONS without Access-Control-Request-Method is not a preflight",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				req := preflightRequest(tb, origin)
				req.Header.Del("Access-Control-Request-Method")
				return req
			},
			wantStatus:    http.StatusTeapot,
			wantReachNext: true,
		},
		{
			name: "actual request reaches the handler and exposes headers",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				req := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/tasks", nil)
				req.Header.Set("Origin", origin)
				return req
			},
			wantStatus:    http.StatusTeapot,
			wantReachNext: true,
			wantHeaders:   map[string]string{"Access-Control-Expose-Headers": "X-Request-ID, Retry-After"},
		},
		{
			name: "actual request from a disallowed origin gets no CORS headers",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				req := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/tasks", nil)
				req.Header.Set("Origin", "https://evil.example.org")
				return req
			},
			wantStatus:    http.StatusTeapot,
			wantReachNext: true,
			wantHeaders:   map[string]string{"Access-Control-Allow-Origin": "", "Access-Control-Expose-Headers": ""},
		},
		{
			name: "actual request without Origin gets no CORS headers",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				return httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/tasks", nil)
			},
			wantStatus:    http.StatusTeapot,
			wantReachNext: true,
			wantHeaders:   map[string]string{"Access-Control-Allow-Origin": "", "Access-Control-Expose-Headers": "", "Vary": ""},
		},
		{
			name: "actual request from an allowed origin gets Allow-Origin",
			opts: CORSMiddlewareOpts{AllowedOrigins: []string{origin}},
			req: func(tb testing.TB) *http.Request {
				tb.Helper()
				req := httptest.NewRequestWithContext(tb.Context(), http.MethodGet, "/tasks", nil)
				req.Header.Set("Origin", origin)
				return req
			},
			wantStatus:    http.StatusTeapot,
			wantReachNext: true,
			wantHeaders:   map[string]string{"Access-Control-Allow-Origin": origin, "Vary": "Origin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusTeapot)
			})

			rec := httptest.NewRecorder()
			corsMiddleware(tt.opts)(next).ServeHTTP(rec, tt.req(t))

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", res.StatusCode, tt.wantStatus)
			}

			if reached != tt.wantReachNext {
				t.Errorf("next handler reached = %t, want %t", reached, tt.wantReachNext)
			}

			for name, want := range tt.wantHeaders {
				if got := res.Header.Get(name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
