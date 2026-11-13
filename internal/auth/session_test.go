package auth

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
	"time"
	"uuid"
)

func TestNewSession(t *testing.T) {
	// non-UTC zone with sub-microsecond precision
	now := time.Date(2026, 10, 1, 19, 0, 0, 123456789, time.FixedZone("UTC+7", 7*3600))
	wantCreatedAt := time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)

	userID := uuid.NewV7()
	hash := sha256Sum("refresh-token")

	tests := []struct {
		name          string
		userAgent     *string
		wantUserAgent *string
	}{
		{name: "no user agent"},
		{name: "user agent", userAgent: new("Mozilla/5.0"), wantUserAgent: new("Mozilla/5.0")},
		{name: "user agent is sanitized", userAgent: new("  curl/8.0\x00  "), wantUserAgent: new("curl/8.0")},
		{name: "user agent is truncated", userAgent: new(strings.Repeat("a", 250)), wantUserAgent: new(strings.Repeat("a", 200))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSession(NewSessionParams{UserID: userID, RefreshTokenHash: hash, UserAgent: tt.userAgent, TTL: 30 * 24 * time.Hour}, now)
			if err != nil {
				t.Fatalf("NewSession() unexpected error: %v", err)
			}

			if got.ID == uuid.Nil() {
				t.Error("ID is the nil UUID")
			}
			if got.UserID != userID {
				t.Errorf("UserID = %v, want %v", got.UserID, userID)
			}
			if !bytes.Equal(got.RefreshTokenHash, hash) {
				t.Errorf("RefreshTokenHash = %x, want %x", got.RefreshTokenHash, hash)
			}
			if got.PreviousRefreshTokenHash != nil {
				t.Errorf("PreviousRefreshTokenHash = %x, want nil", got.PreviousRefreshTokenHash)
			}
			if got.RotatedAt != nil {
				t.Errorf("RotatedAt = %v, want nil", got.RotatedAt)
			}
			if !got.CreatedAt.Equal(wantCreatedAt) || got.CreatedAt.Location() != time.UTC {
				t.Errorf("CreatedAt = %v, want %v (UTC, microseconds)", got.CreatedAt, wantCreatedAt)
			}
			if want := wantCreatedAt.Add(30 * 24 * time.Hour); !got.ExpiresAt.Equal(want) {
				t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want)
			}

			switch {
			case tt.wantUserAgent == nil && got.UserAgent != nil:
				t.Errorf("UserAgent = %q, want nil", *got.UserAgent)
			case tt.wantUserAgent != nil && got.UserAgent == nil:
				t.Errorf("UserAgent = nil, want %q", *tt.wantUserAgent)
			case tt.wantUserAgent != nil && *got.UserAgent != *tt.wantUserAgent:
				t.Errorf("UserAgent = %q, want %q", *got.UserAgent, *tt.wantUserAgent)
			}
		})
	}
}

func TestNewSession_InvalidRefreshTokenHash(t *testing.T) {
	tests := []struct {
		name string
		hash []byte
	}{
		{name: "nil", hash: nil},
		{name: "empty", hash: []byte{}},
		{name: "too short", hash: make([]byte, sha256.Size-1)},
		{name: "too long", hash: make([]byte, sha256.Size+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ua := "  curl/8.0  "
			got, err := NewSession(NewSessionParams{UserID: uuid.NewV7(), RefreshTokenHash: tt.hash, UserAgent: &ua}, time.Now())

			if err == nil {
				t.Fatal("NewSession() error = nil, want an error")
			}
			if got.ID != uuid.Nil() {
				t.Errorf("NewSession() = %+v, want zero value on error", got)
			}
			if ua != "  curl/8.0  " {
				t.Errorf("user agent was modified to %q despite the error", ua)
			}
		})
	}
}

func TestNewSession_UniqueIDs(t *testing.T) {
	p := NewSessionParams{UserID: uuid.NewV7(), RefreshTokenHash: sha256Sum("refresh-token")}

	a, err := NewSession(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSession(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if a.ID == b.ID {
		t.Errorf("two sessions share the ID %v", a.ID)
	}
}

func TestSanitizeUserAgent(t *testing.T) {
	tests := []struct {
		name     string
		ua       string
		maxChars int
		want     string
	}{
		{name: "plain", ua: "Mozilla/5.0", maxChars: 200, want: "Mozilla/5.0"},
		{name: "empty", ua: "", maxChars: 200, want: ""},
		{name: "only whitespace", ua: " \t\n ", maxChars: 200, want: ""},
		{name: "trimmed", ua: "  curl/8.0 \n", maxChars: 200, want: "curl/8.0"},
		{name: "NUL bytes removed", ua: "cu\x00rl/\x008.0", maxChars: 200, want: "curl/8.0"},
		{name: "only NUL bytes", ua: "\x00\x00", maxChars: 200, want: ""},
		{name: "invalid UTF-8 removed", ua: "ab\xffcd\xc3", maxChars: 200, want: "abcd"},
		{name: "valid multibyte kept", ua: "café 密碼", maxChars: 200, want: "café 密碼"},

		{name: "exactly max characters", ua: "abcde", maxChars: 5, want: "abcde"},
		{name: "one over max characters", ua: "abcdef", maxChars: 5, want: "abcde"},
		{name: "truncated to max characters", ua: strings.Repeat("a", 300), maxChars: 200, want: strings.Repeat("a", 200)},
		{name: "trimmed again after truncation", ua: "ab   cd", maxChars: 4, want: "ab"},
		{
			// 5 runes but 10 bytes: shorter than max in characters, longer in bytes
			name: "multibyte within max characters", ua: strings.Repeat("é", 5), maxChars: 5, want: strings.Repeat("é", 5),
		},
		{
			name: "multibyte truncated by characters, not bytes", ua: strings.Repeat("é", 5), maxChars: 3, want: strings.Repeat("é", 3),
		},
		{
			name: "NUL and invalid bytes do not count towards max", ua: "a\x00b\xffc", maxChars: 3, want: "abc",
		},

		{name: "zero max", ua: "abc", maxChars: 0, want: ""},
		{name: "negative max", ua: "abc", maxChars: -1, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeUserAgent(tt.ua, tt.maxChars)

			if got != tt.want {
				t.Errorf("sanitizeUserAgent(%q, %d) = %q, want %q", tt.ua, tt.maxChars, got, tt.want)
			}
		})
	}
}

func TestSession_IsExpired(t *testing.T) {
	expiresAt := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	s := Session{ExpiresAt: expiresAt}

	tests := []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "long before", now: expiresAt.Add(-24 * time.Hour), want: false},
		{name: "just before", now: expiresAt.Add(-time.Microsecond), want: false},
		{name: "exactly at expiry", now: expiresAt, want: true},
		{name: "just after", now: expiresAt.Add(time.Microsecond), want: true},
		{name: "long after", now: expiresAt.Add(24 * time.Hour), want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.IsExpired(tt.now); got != tt.want {
				t.Errorf("IsExpired(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestSession_MatchRefresh(t *testing.T) {
	const grace = 10 * time.Second

	current := []byte("current-hash")
	previous := []byte("previous-hash")
	rotatedAt := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	fresh := Session{RefreshTokenHash: current}
	rotated := Session{RefreshTokenHash: current, PreviousRefreshTokenHash: previous, RotatedAt: &rotatedAt}

	tests := []struct {
		name    string
		session Session
		hash    []byte
		now     time.Time
		want    RefreshMatch
	}{
		{name: "current hash", session: fresh, hash: current, now: rotatedAt, want: RefreshCurrent},
		{name: "current hash on a rotated session", session: rotated, hash: current, now: rotatedAt.Add(time.Hour), want: RefreshCurrent},
		{name: "unknown hash", session: rotated, hash: []byte("other-hash"), now: rotatedAt, want: RefreshNone},
		{name: "unknown hash on a fresh session", session: fresh, hash: []byte("other-hash"), now: rotatedAt, want: RefreshNone},
		{name: "empty hash on a fresh session", session: fresh, hash: nil, now: rotatedAt, want: RefreshNone},
		{name: "prefix of the current hash", session: fresh, hash: current[:5], now: rotatedAt, want: RefreshNone},

		{name: "previous hash right after rotation", session: rotated, hash: previous, now: rotatedAt, want: RefreshGrace},
		{name: "previous hash inside the grace period", session: rotated, hash: previous, now: rotatedAt.Add(grace - time.Second), want: RefreshGrace},
		{name: "previous hash exactly at the end of the grace period", session: rotated, hash: previous, now: rotatedAt.Add(grace), want: RefreshGrace},
		{name: "previous hash after the grace period", session: rotated, hash: previous, now: rotatedAt.Add(grace + time.Second), want: RefreshReuse},
		{
			name:    "previous hash without a rotation time",
			session: Session{RefreshTokenHash: current, PreviousRefreshTokenHash: previous},
			hash:    previous,
			now:     rotatedAt,
			want:    RefreshReuse,
		},
		{name: "empty hash on a rotated session", session: rotated, hash: nil, now: rotatedAt, want: RefreshNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.session.MatchRefresh(tt.hash, tt.now, grace); got != tt.want {
				t.Errorf("MatchRefresh() = %v, want %v", got, tt.want)
			}
		})
	}
}

func sha256Sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}
