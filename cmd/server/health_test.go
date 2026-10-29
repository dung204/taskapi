package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
)

type healthResponse struct {
	Status string `json:"status"`
	Store  string `json:"store"`
}

const (
	statusOK          = "ok"
	statusUnavailable = "unavailable"
)

type mockPinger struct{ err error }

func (p mockPinger) Ping(context.Context) error { return p.err }

func checkHealthResponse(tb testing.TB, got, want healthResponse, opts ...cmp.Option) {
	tb.Helper()
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("health response mismatch (-want +got):\n%s", diff)
	}
}

func TestHealthHandler(t *testing.T) {
	tests := []struct {
		name           string
		pinger         storePinger
		wantStatusCode int
		wantResponse   healthResponse
	}{
		{"pinger nil", nil, http.StatusOK, healthResponse{Status: statusOK, Store: statusOK}},
		{"pinger ok", mockPinger{}, http.StatusOK, healthResponse{Status: statusOK, Store: statusOK}},
		{"pinger error", mockPinger{err: errors.New("boom")}, http.StatusServiceUnavailable,
			healthResponse{Status: statusUnavailable, Store: statusUnavailable}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()
			healthHandler(tt.pinger).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Fatalf("status code = %v, want %v", rec.Code, tt.wantStatusCode)
			}
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}

			var got healthResponse
			dec := json.NewDecoder(rec.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(&got); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			checkHealthResponse(t, got, tt.wantResponse)
		})
	}
}
