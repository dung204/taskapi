package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestInternalHandlers(t *testing.T) {
	reg := prometheus.NewRegistry()
	marker := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_marker_total",
		Help: "marker",
	})
	reg.MustRegister(marker)

	boom := mockPinger{err: errors.New("boom")}

	tests := []struct {
		name           string
		handler        http.Handler
		method, path   string
		wantStatusCode int
	}{
		{"healthz wired to the pinger", opsHandler(reg, boom), "GET", "/healthz", 503},
		{"healthz without pinger", opsHandler(reg, nil), "GET", "/healthz", 200},
		{"healthz is GET only", opsHandler(reg, nil), "POST", "/healthz", 405},
		{"metrics served from reg", opsHandler(reg, nil), "GET", "/metrics", 200},
		{"pprof not on the ops port", opsHandler(reg, nil), "GET", "/debug/pprof/", 404},
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
