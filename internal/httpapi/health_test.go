package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type healthResponse struct {
	Status string `json:"status"`
	Store  string `json:"store"`
}

var okStr = "ok"
var unavailableStr = "unavailable"

type mockPinger struct{ err error }

func (p mockPinger) Ping(context.Context) error { return p.err }

func checkHealthResponse(t *testing.T, got healthResponse, want healthResponse) {
	t.Helper()
	if got.Status != want.Status {
		t.Errorf("status = %q, want %q", got.Status, want.Status)
	}
	if got.Store != want.Store {
		t.Errorf("status = %q, want %q", got.Store, want.Store)
	}
}

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name           string
		pinger         storePinger
		wantStatusCode int
		wantResponse   healthResponse
	}{
		{"pinger nil", nil, http.StatusOK, healthResponse{Status: okStr, Store: okStr}},
		{"pinger ok", mockPinger{}, http.StatusOK, healthResponse{Status: okStr, Store: okStr}},
		{"pinger error", mockPinger{err: errors.New("boom")}, http.StatusServiceUnavailable, healthResponse{Status: unavailableStr, Store: unavailableStr}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, _, _ := setupTestWithPinger(t, tt.pinger)
			rec, req := prepareTestRequest(t, http.MethodGet, "/healthz", nil)
			h.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()

			if res.StatusCode != tt.wantStatusCode {
				t.Fatalf("status code = %v, want %v", res.StatusCode, tt.wantStatusCode)
			}

			checkHealthResponse(t, decodeResponseBody[healthResponse](t, res.Body), tt.wantResponse)
		})
	}
}
