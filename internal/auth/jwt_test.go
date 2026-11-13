package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"slices"
	"testing"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testIssuer    = "taskapi"
	testAudience  = "taskapi-clients"
	testAudience2 = "taskapi-admin"
	testTTL       = 15 * time.Minute
)

var (
	testUserID    = uuid.NewV7()
	testSessionID = uuid.NewV7()
	// whole seconds, since JWT timestamps have a precision of one second
	testNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
)

func newTestKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func newTestService(t *testing.T) (*JWTService, ed25519.PrivateKey) {
	t.Helper()

	pub, priv := newTestKeys(t)
	return NewJWTService(JWTServiceOpts{
		PrivateKey: priv,
		PublicKey:  pub,
		Issuer:     testIssuer,
		Audiences:  []string{testAudience},
		TTL:        testTTL,
	}), priv
}

// validClaims returns claims that Verify accepts when called with testNow.
func validClaims() jwtClaims {
	return jwtClaims{
		SessionID: testSessionID.String(),
		Issuer:    testIssuer,
		Audience:  jwt.ClaimStrings{testAudience},
		Subject:   testUserID.String(),
		IssuedAt:  jwt.NewNumericDate(testNow),
		ExpiresAt: jwt.NewNumericDate(testNow.Add(testTTL)),
	}
}

func signClaims(t *testing.T, method jwt.SigningMethod, key any, claims jwt.Claims) string {
	t.Helper()

	signed, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func TestIssue(t *testing.T) {
	s, _ := newTestService(t)

	tokenStr, expiresAt, err := s.Issue(testUserID, testSessionID, testNow)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	if want := testNow.Add(testTTL); !expiresAt.Equal(want) {
		t.Errorf("Issue() expiresAt = %v, want %v", expiresAt, want)
	}

	got := &jwtClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, got,
		func(*jwt.Token) (any, error) { return s.publicKey, nil },
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithTimeFunc(func() time.Time { return testNow }),
	)
	if err != nil {
		t.Fatalf("issued token does not parse: %v", err)
	}
	if !token.Valid {
		t.Fatal("issued token is not valid")
	}

	if got.Subject != testUserID.String() {
		t.Errorf("sub = %q, want %q", got.Subject, testUserID)
	}
	if got.SessionID != testSessionID.String() {
		t.Errorf("sid = %q, want %q", got.SessionID, testSessionID)
	}
	if got.Issuer != testIssuer {
		t.Errorf("iss = %q, want %q", got.Issuer, testIssuer)
	}
	if len(got.Audience) != 1 || got.Audience[0] != testAudience {
		t.Errorf("aud = %v, want [%q]", got.Audience, testAudience)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(testNow.Add(testTTL)) {
		t.Errorf("exp = %v, want %v", got.ExpiresAt, testNow.Add(testTTL))
	}
	// Verify requires iat, so Issue must set it.
	if got.IssuedAt == nil || !got.IssuedAt.Equal(testNow) {
		t.Errorf("iat = %v, want %v", got.IssuedAt, testNow)
	}
}

func TestIssueMultipleAudiences(t *testing.T) {
	pub, priv := newTestKeys(t)
	s := NewJWTService(JWTServiceOpts{
		PrivateKey: priv,
		PublicKey:  pub,
		Issuer:     testIssuer,
		Audiences:  []string{testAudience, testAudience2},
		TTL:        testTTL,
	})

	tokenStr, _, err := s.Issue(testUserID, testSessionID, testNow)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	got := &jwtClaims{}
	if _, err := jwt.ParseWithClaims(tokenStr, got,
		func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithTimeFunc(func() time.Time { return testNow }),
	); err != nil {
		t.Fatalf("issued token does not parse: %v", err)
	}

	want := []string{testAudience, testAudience2}
	if !slices.Equal(got.Audience, want) {
		t.Errorf("aud = %v, want %v", got.Audience, want)
	}
}

// Verify accepts a token when its aud claim contains any one of the configured
// audiences.
func TestVerifyMultipleAudiences(t *testing.T) {
	pub, priv := newTestKeys(t)
	s := NewJWTService(JWTServiceOpts{
		PrivateKey: priv,
		PublicKey:  pub,
		Issuer:     testIssuer,
		Audiences:  []string{testAudience, testAudience2},
		TTL:        testTTL,
	})

	tests := []struct {
		name     string
		audience jwt.ClaimStrings
		wantErr  error
	}{
		{name: "first configured audience", audience: jwt.ClaimStrings{testAudience}},
		{name: "second configured audience", audience: jwt.ClaimStrings{testAudience2}},
		{name: "both configured audiences", audience: jwt.ClaimStrings{testAudience, testAudience2}},
		{name: "one configured among others", audience: jwt.ClaimStrings{"someone-else", testAudience2}},
		{name: "none configured", audience: jwt.ClaimStrings{"someone-else"}, wantErr: ErrInvalidToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validClaims()
			c.Audience = tt.audience

			_, err := s.Verify(signClaims(t, jwt.SigningMethodEdDSA, priv, c), testNow)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestIssueVerify(t *testing.T) {
	s, _ := newTestService(t)

	tokenStr, _, err := s.Issue(testUserID, testSessionID, testNow)
	if err != nil {
		t.Fatalf("Issue() unexpected error: %v", err)
	}

	got, err := s.Verify(tokenStr, testNow.Add(time.Minute))
	if err != nil {
		t.Fatalf("Verify() unexpected error: %v", err)
	}

	want := Claims{UserID: testUserID, SessionID: testSessionID, IssuedAt: testNow}
	if got.UserID != want.UserID {
		t.Errorf("UserID = %v, want %v", got.UserID, want.UserID)
	}
	if got.SessionID != want.SessionID {
		t.Errorf("SessionID = %v, want %v", got.SessionID, want.SessionID)
	}
	if !got.IssuedAt.Equal(want.IssuedAt) {
		t.Errorf("IssuedAt = %v, want %v", got.IssuedAt, want.IssuedAt)
	}
}

func TestVerify(t *testing.T) {
	s, priv := newTestService(t)
	_, otherPriv := newTestKeys(t)

	tests := []struct {
		name    string
		token   func(t *testing.T) string
		now     time.Time // defaults to testNow
		wantErr error
	}{
		{
			name: "valid",
			token: func(t *testing.T) string {
				t.Helper()
				return signClaims(t, jwt.SigningMethodEdDSA, priv, validClaims())
			},
		},

		// expiry
		{
			name: "expired",
			token: func(t *testing.T) string {
				t.Helper()
				return signClaims(t, jwt.SigningMethodEdDSA, priv, validClaims())
			},
			now:     testNow.Add(testTTL + time.Minute),
			wantErr: ErrExpiredToken,
		},
		{
			name: "just expired but within the 5s leeway",
			token: func(t *testing.T) string {
				t.Helper()
				return signClaims(t, jwt.SigningMethodEdDSA, priv, validClaims())
			},
			now: testNow.Add(testTTL + 4*time.Second),
		},
		{
			name: "expired beyond the leeway",
			token: func(t *testing.T) string {
				t.Helper()
				return signClaims(t, jwt.SigningMethodEdDSA, priv, validClaims())
			},
			now:     testNow.Add(testTTL + 6*time.Second),
			wantErr: ErrExpiredToken,
		},
		{
			name: "missing exp",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.ExpiresAt = nil
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},

		// signature and algorithm
		{
			name: "signed with another key",
			token: func(t *testing.T) string {
				t.Helper()
				return signClaims(t, jwt.SigningMethodEdDSA, otherPriv, validClaims())
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "tampered payload",
			token: func(t *testing.T) string {
				t.Helper()
				valid := signClaims(t, jwt.SigningMethodEdDSA, priv, validClaims())
				forged := signClaims(t, jwt.SigningMethodEdDSA, otherPriv, func() jwtClaims {
					c := validClaims()
					c.Subject = uuid.NewV7().String()
					return c
				}())
				// header and signature of the valid token, payload of the forged one
				return splitJoin(valid, forged)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "HS256 signed with the public key",
			token: func(t *testing.T) string {
				t.Helper()
				return signClaims(t, jwt.SigningMethodHS256, []byte(s.publicKey), validClaims())
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "alg none",
			token: func(t *testing.T) string {
				t.Helper()
				return signClaims(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, validClaims())
			},
			wantErr: ErrInvalidToken,
		},

		// issuer and audience
		{
			name: "wrong issuer",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Issuer = "someone-else"
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "missing issuer",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Issuer = ""
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "wrong audience",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Audience = jwt.ClaimStrings{"someone-else"}
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "missing audience",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Audience = nil
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "audience list containing ours",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Audience = jwt.ClaimStrings{"someone-else", testAudience}
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
		},

		// subject, session and issued-at
		{
			name: "missing sub",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Subject = ""
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "sub is not a UUID",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Subject = "not-a-uuid"
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "sub is the nil UUID",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.Subject = uuid.Nil().String()
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "missing sid",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.SessionID = ""
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "sid is not a UUID",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.SessionID = "not-a-uuid"
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "sid is the nil UUID",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.SessionID = uuid.Nil().String()
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},
		{
			name: "missing iat",
			token: func(t *testing.T) string {
				t.Helper()
				c := validClaims()
				c.IssuedAt = nil
				return signClaims(t, jwt.SigningMethodEdDSA, priv, c)
			},
			wantErr: ErrInvalidToken,
		},

		// malformed input
		{name: "empty", token: func(*testing.T) string { return "" }, wantErr: ErrInvalidToken},
		{name: "garbage", token: func(*testing.T) string { return "not.a.jwt" }, wantErr: ErrInvalidToken},
		{name: "one segment", token: func(*testing.T) string { return "abc" }, wantErr: ErrInvalidToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := tt.now
			if now.IsZero() {
				now = testNow
			}

			got, err := s.Verify(tt.token(t), now)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Verify() error = %v, want %v", err, tt.wantErr)
				}
				if got != (Claims{}) {
					t.Errorf("Verify() = %+v, want zero Claims on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Verify() unexpected error: %v", err)
			}
			if got.UserID != testUserID {
				t.Errorf("UserID = %v, want %v", got.UserID, testUserID)
			}
			if got.SessionID != testSessionID {
				t.Errorf("SessionID = %v, want %v", got.SessionID, testSessionID)
			}
			if !got.IssuedAt.Equal(testNow) {
				t.Errorf("IssuedAt = %v, want %v", got.IssuedAt, testNow)
			}
		})
	}
}

// splitJoin returns the header and signature of a with the payload of b.
func splitJoin(a, b string) string {
	seg := func(s string) [3]string {
		var out [3]string
		i := 0
		start := 0
		for j := 0; j < len(s) && i < 2; j++ {
			if s[j] == '.' {
				out[i] = s[start:j]
				i++
				start = j + 1
			}
		}
		out[2] = s[start:]
		return out
	}

	sa, sb := seg(a), seg(b)
	return sa[0] + "." + sb[1] + "." + sa[2]
}
