package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dung204/taskapi/internal/httpapi/oapigen"
)

type mockPinger struct{ err error }

func (p mockPinger) Ping(context.Context) error { return p.err }

func checkHealthResponse(tb testing.TB, got, want oapigen.Health, opts ...cmp.Option) {
	tb.Helper()
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("health response mismatch (-want +got):\n%s", diff)
	}
}

func TestCheckHealth(t *testing.T) {
	tests := []struct {
		name           string
		pinger         storePinger
		wantStatusCode int
		wantResponse   oapigen.Health
	}{
		{"pinger nil", nil, http.StatusOK, oapigen.Health{Status: oapigen.HealthStatusOk, Store: oapigen.HealthStoreOk}},
		{"pinger ok", mockPinger{}, http.StatusOK, oapigen.Health{Status: oapigen.HealthStatusOk, Store: oapigen.HealthStoreOk}},
		{"pinger error", mockPinger{err: errors.New("boom")}, http.StatusServiceUnavailable,
			oapigen.Health{Status: oapigen.HealthStatusUnavailable, Store: oapigen.HealthStoreUnavailable}},
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

			checkHealthResponse(t, decodeResponseBody[oapigen.Health](t, res.Body), tt.wantResponse)
		})
	}
}
