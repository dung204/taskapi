package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestByIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
		wantOK     bool
	}{
		{name: "IPv4", remoteAddr: "192.0.2.1:1234", want: "ip:192.0.2.1", wantOK: true},
		{name: "IPv4-mapped IPv6", remoteAddr: "[::ffff:192.0.2.1]:1234", want: "ip:192.0.2.1", wantOK: true},
		{name: "IPv6 is grouped by /64", remoteAddr: "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:1234", want: "ip:2001:db8:1:2::", wantOK: true},
		{name: "same /64, different host", remoteAddr: "[2001:db8:1:2::1]:1234", want: "ip:2001:db8:1:2::", wantOK: true},
		{name: "missing port", remoteAddr: "192.0.2.1", wantOK: false},
		{name: "not an IP", remoteAddr: "localhost:1234", wantOK: false},
		{name: "empty", remoteAddr: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr

			got, gotOK := byIP(req)
			if got != tt.want || gotOK != tt.wantOK {
				t.Errorf("byIP(%q) = (%q, %t), want (%q, %t)", tt.remoteAddr, got, gotOK, tt.want, tt.wantOK)
			}
		})
	}
}

func TestRateLimitRetryAfter(t *testing.T) {
	tests := []struct {
		retryAfter time.Duration
		want       int
	}{
		{0, 1},
		{time.Millisecond, 1},
		{time.Second, 1},
		{time.Second + time.Millisecond, 2},
		{90 * time.Second, 90},
	}

	for _, tt := range tests {
		t.Run(tt.retryAfter.String(), func(t *testing.T) {
			l := &fakeLimiter{ok: false, retryAfter: tt.retryAfter}
			h := rateLimit(l, byIP)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("next handler called for a limited request")
			}))

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			h.ServeHTTP(rec, req)

			if got := rec.Header().Get("Retry-After"); got != strconv.Itoa(tt.want) {
				t.Errorf("Retry-After for %v = %q, want %d", tt.retryAfter, got, tt.want)
			}
		})
	}
}
