package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/testutil/logtest"
)

func TestInternalHandlers(t *testing.T) {
	reg := prometheus.NewRegistry()
	marker := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_marker_total",
		Help: "marker",
	})
	reg.MustRegister(marker)

	logger, _ := logtest.New()
	critical := newCheck("postgres", true, failPing)
	optional := newCheck("redis", false, failPing)

	tests := []struct {
		name           string
		handler        http.Handler
		method, path   string
		wantStatusCode int
	}{
		{"healthz wired to a critical check", opsHandler(reg, logger, critical), "GET", "/healthz", 503},
		{"healthz with a failing optional check", opsHandler(reg, logger, optional), "GET", "/healthz", 200},
		{"healthz without checks", opsHandler(reg, logger), "GET", "/healthz", 200},
		{"healthz is GET only", opsHandler(reg, logger), "POST", "/healthz", 405},
		{"metrics served from reg", opsHandler(reg, logger), "GET", "/metrics", 200},
		{"pprof not on the ops port", opsHandler(reg, logger), "GET", "/debug/pprof/", 404},
		{"pprof served", pprofHandler(), "GET", "/debug/pprof/", 200},
		{"pprof heap served", pprofHandler(), "GET", "/debug/pprof/heap", 200},
		{"metrics not on the pporf port", pprofHandler(), "GET", "/metrics", 404},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			tt.handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Fatalf("%s %s: status = %d, want %d", tt.method, tt.path, rec.Code, tt.wantStatusCode)
			}
		})
	}
}
