package httpapi

// Black-box tests for the "auth" group of openapi.yml. They only talk HTTP; the
// single place that knows how the handler, stores, mailer and clock are built
// is setupAuthTest. Wire it to the real implementation, then make the tests
// pass one endpoint at a time.

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"uuid"

	"golang.org/x/time/rate"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/ratelimit"
	"github.com/dung204/taskapi/internal/store/memory"
)

const (
	testPassword    = "correct horse battery"
	testNewPassword = "new password 123"
	testEmail       = "alice@example.com"
)

// Kinds of emails the fake mailer records.
const (
	mailVerifyEmail       = "verify_email"
	mailAlreadyRegistered = "already_registered"
	mailResetPassword     = "reset_password"
)

type sentMail struct {
	Kind  string
	Token string // one-time token; empty for mails that carry none
}

type userState struct {
	Verified bool
	Disabled bool
}

// authEnv is everything a test needs besides plain HTTP.
type authEnv struct {
	h http.Handler

	// AllowedOrigin is an origin the server accepts in cookie mode.
	AllowedOrigin string

	// AddUser inserts a user with a hashed password directly into the store.
	AddUser func(tb testing.TB, email, password string, st userState) auth.User
	// Mails returns, oldest first, the emails sent to the address so far.
	Mails func(tb testing.TB, email string) []sentMail
	// Disable marks an existing user as disabled.
	Disable func(tb testing.TB, userID uuid.UUID)
	// Advance moves the server clock forward.
	Advance func(d time.Duration)
	// ForgeToken makes an access token for the user that the server must
	// reject: kind is "expired" or "wrong_key".
	ForgeToken func(tb testing.TB, userID uuid.UUID, kind string) string
}

// setupAuthTest builds a handler backed by in-memory stores, a fake mailer and
// a controllable clock. Rate limits should be small enough that the tests
// below reach them within rateLimitProbes requests.
func setupAuthTest(t *testing.T) *authEnv {
	t.Helper()

	const (
		accessTTL     = 15 * time.Minute
		allowedOrigin = "https://app.example.com"
	)

	var (
		clock   = newTestClock()
		db      = newFakeDB()
		revoker = newFakeRevoker()
	)

	pub, priv := newTestKeys(t)
	jwtOpts := auth.JWTServiceOpts{
		PrivateKey: priv, PublicKey: pub,
		Issuer: "taskapi-test", Audiences: []string{"taskapi-test"}, TTL: accessTTL,
	}
	jwtService := auth.NewJWTService(jwtOpts)

	service, err := auth.NewService(auth.ServiceDeps{
		RefreshTTL:         30 * 24 * time.Hour,
		RefreshGracePeriod: refreshGrace,
		Tx:                 db,
		Sessions:           fakeSessions{db},
		UserTokens:         fakeUserTokens{db},
		Users:              fakeUsers{db},
		Outbox:             fakeOutbox{db},
		AccessTokenIssuer:  jwtService,
		Hasher:             fakeHasher{},
		Revoker:            revoker,
		LoginLimiter:       newFakeLoginLimiter(clock.Now, 5, time.Minute),
		AuthMailLimiter:    newFakeMailLimiter(clock.Now, 5),
	})
	if err != nil {
		t.Fatalf("new auth service: %v", err)
	}

	h := NewHandler(HandlerDeps{
		Tasks:    newTestService(memory.NewTaskStore()),
		Auth:     service,
		Verifier: jwtService,
		Revoker:  revoker,
		Logger:   slog.New(slog.DiscardHandler),
		// A small burst so the per-IP tests reach a 429 within
		// rateLimitProbes requests; the refill is negligible during a test.
		Limiter: ratelimit.NewMemoryLimiter(rate.Limit(0.01), 30),
		Origins: NewOriginPolicy([]string{allowedOrigin}),
		Now:     clock.Now,
	})

	return &authEnv{
		h:             h,
		AllowedOrigin: allowedOrigin,
		AddUser: func(tb testing.TB, mail, password string, st userState) auth.User {
			tb.Helper()
			hash, err := fakeHasher{}.Hash(tb.Context(), password)
			if err != nil {
				tb.Fatalf("hash password: %v", err)
			}
			u, err := auth.NewUser(auth.NewUserParams{Email: mail, PasswordHash: &hash}, clock.Now())
			if err != nil {
				tb.Fatalf("new user: %v", err)
			}
			if st.Verified {
				now := clock.Now()
				u.EmailVerifiedAt = &now
			}
			if st.Disabled {
				now := clock.Now()
				u.DisabledAt = &now
			}
			if err := (fakeUsers{db}).Create(tb.Context(), u); err != nil {
				tb.Fatalf("create user: %v", err)
			}
			return u
		},
		Mails: func(tb testing.TB, mail string) []sentMail {
			tb.Helper()
			db.deliver(tb, clock.Now())
			db.mu.Lock()
			defer db.mu.Unlock()
			return slices.Clone(db.mails[strings.ToLower(mail)])
		},
		Disable: func(tb testing.TB, userID uuid.UUID) {
			tb.Helper()
			err := (fakeUsers{db}).update(userID, func(u *auth.User) {
				now := clock.Now()
				u.DisabledAt = &now
			})
			if err != nil {
				tb.Fatalf("disable user: %v", err)
			}
		},
		Advance: clock.Advance,
		ForgeToken: func(tb testing.TB, userID uuid.UUID, kind string) string {
			tb.Helper()
			var (
				svc = jwtService
				iat = clock.Now()
			)
			switch kind {
			case "expired":
				iat = iat.Add(-2 * accessTTL)
			case "wrong_key":
				otherPub, otherPriv := newTestKeys(tb)
				opts := jwtOpts
				opts.PublicKey, opts.PrivateKey = otherPub, otherPriv
				svc = auth.NewJWTService(opts)
			default:
				tb.Fatalf("unknown forged token kind %q", kind)
			}
			token, _, err := svc.Issue(userID, uuid.NewV7(), iat)
			if err != nil {
				tb.Fatalf("issue forged token: %v", err)
			}
			return token
		},
	}
}

// rateLimitProbes is the most requests a test sends while looking for a 429.
const rateLimitProbes = 100

// --- request helpers ---------------------------------------------------------

type reqOpt func(*http.Request)

func withBearer(token string) reqOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

func withHeader(k, v string) reqOpt {
	return func(r *http.Request) { r.Header.Set(k, v) }
}

func withOrigin(o string) reqOpt { return withHeader("Origin", o) }

func withRefreshCookie(v string) reqOpt {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "refresh_token", Value: v}) }
}

func withRemoteAddr(addr string) reqOpt {
	return func(r *http.Request) { r.RemoteAddr = addr }
}

type apiResp struct {
	Status int
	Header http.Header
	Cookie []*http.Cookie
	Body   []byte
}

func (e *authEnv) do(tb testing.TB, method, path string, body any, opts ...reqOpt) apiResp {
	tb.Helper()
	rec, req := prepareTestRequest(tb, method, path, body)
	for _, o := range opts {
		o(req)
	}
	e.h.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		tb.Fatalf("read response body: %v", err)
	}
	return apiResp{Status: res.StatusCode, Header: res.Header, Cookie: res.Cookies(), Body: b}
}

func (r apiResp) errorMessage(tb testing.TB) string {
	tb.Helper()
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(r.Body, &e); err != nil || e.Error == "" {
		tb.Fatalf("body is not an error response: %q (%v)", r.Body, err)
	}
	return e.Error
}

func (r apiResp) cookie(name string) *http.Cookie {
	for _, c := range r.Cookie {
		if c.Name == name {
			return c
		}
	}
	return nil
}

type tokenResp struct {
	AccessToken  string  `json:"access_token"`
	TokenType    string  `json:"token_type"`
	ExpiresIn    int64   `json:"expires_in"`
	RefreshToken *string `json:"refresh_token"`
}

func decodeTokens(tb testing.TB, r apiResp) tokenResp {
	tb.Helper()
	var tr tokenResp
	if err := json.Unmarshal(r.Body, &tr); err != nil {
		tb.Fatalf("decode token response %q: %v", r.Body, err)
	}
	return tr
}

func wantStatus(tb testing.TB, r apiResp, want int) {
	tb.Helper()
	if r.Status != want {
		tb.Fatalf("status = %d, want %d (body %q)", r.Status, want, r.Body)
	}
}

func wantEmptyBody(tb testing.TB, r apiResp) {
	tb.Helper()
	if len(r.Body) != 0 {
		tb.Errorf("body = %q, want empty", r.Body)
	}
}

func wantRetryAfter(tb testing.TB, r apiResp) {
	tb.Helper()
	n, err := strconv.Atoi(r.Header.Get("Retry-After"))
	if err != nil || n < 1 {
		tb.Errorf("Retry-After = %q, want an integer >= 1", r.Header.Get("Retry-After"))
	}
}

func wantClearedCookie(tb testing.TB, r apiResp) {
	tb.Helper()
	c := r.cookie("refresh_token")
	if c == nil {
		tb.Fatal("refresh_token cookie is not cleared (no Set-Cookie)")
	}
	if c.Value != "" || (c.MaxAge >= 0 && !c.Expires.Before(time.Now())) {
		tb.Errorf("refresh_token cookie = %+v, want an expired cookie with an empty value", c)
	}
}

// --- flow helpers ------------------------------------------------------------

func (e *authEnv) verifiedUser(tb testing.TB, email string) auth.User {
	tb.Helper()
	return e.AddUser(tb, email, testPassword, userState{Verified: true})
}

// loginToken signs in in token mode and returns the tokens.
func (e *authEnv) loginToken(tb testing.TB, email, password string) tokenResp {
	tb.Helper()
	r := e.do(tb, http.MethodPost, "/auth/login", map[string]any{"email": email, "password": password})
	wantStatus(tb, r, http.StatusOK)
	tr := decodeTokens(tb, r)
	if tr.RefreshToken == nil {
		tb.Fatal("login in token mode returned no refresh_token")
	}
	return tr
}

// loginCookie signs in in cookie mode and returns the refresh cookie value.
func (e *authEnv) loginCookie(tb testing.TB, email, password string) (tokenResp, string) {
	tb.Helper()
	r := e.do(tb, http.MethodPost, "/auth/login", map[string]any{"email": email, "password": password, "token_mode": "cookie"})
	wantStatus(tb, r, http.StatusOK)
	c := r.cookie("refresh_token")
	if c == nil || c.Value == "" {
		tb.Fatal("login in cookie mode set no refresh_token cookie")
	}
	return decodeTokens(tb, r), c.Value
}

func (e *authEnv) refreshWithBody(tb testing.TB, token string) apiResp {
	tb.Helper()
	return e.do(tb, http.MethodPost, "/auth/refresh", map[string]any{"refresh_token": token})
}

func (e *authEnv) lastMail(tb testing.TB, email, kind string) sentMail {
	tb.Helper()
	mails := e.Mails(tb, email)
	for _, mail := range slices.Backward(mails) {
		if mail.Kind == kind {
			return mail
		}
	}
	tb.Fatalf("no %q mail sent to %s (got %+v)", kind, email, mails)
	return sentMail{}
}

func (e *authEnv) meStatus(tb testing.TB, accessToken string) int {
	tb.Helper()
	return e.do(tb, http.MethodGet, "/me", nil, withBearer(accessToken)).Status
}

// --- POST /auth/register -----------------------------------------------------

func TestRegister(t *testing.T) {
	t.Run("unknown email: 202, empty body, verification mail", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": testEmail, "password": testPassword})
		wantStatus(t, r, http.StatusAccepted)
		wantEmptyBody(t, r)

		m := e.lastMail(t, testEmail, mailVerifyEmail)
		if m.Token == "" {
			t.Error("verification mail carries no token")
		}
	})

	t.Run("email is normalized", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": "  Alice@Example.COM ", "password": testPassword})
		wantStatus(t, r, http.StatusAccepted)

		m := e.lastMail(t, testEmail, mailVerifyEmail)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": m.Token}), http.StatusNoContent)
		e.loginToken(t, "ALICE@example.com", testPassword)
	})

	t.Run("existing email: same 202, no verification mail, 'already registered' mail", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		r := e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": testEmail, "password": "another password"})
		wantStatus(t, r, http.StatusAccepted)
		wantEmptyBody(t, r)

		var kinds []string
		for _, m := range e.Mails(t, testEmail) {
			kinds = append(kinds, m.Kind)
		}
		if got := strings.Join(kinds, ","); got != mailAlreadyRegistered {
			t.Errorf("mails = %q, want only %q", got, mailAlreadyRegistered)
		}

		// The existing password must not have been replaced.
		e.loginToken(t, testEmail, testPassword)
	})

	t.Run("account cannot sign in before verification", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": testEmail, "password": testPassword}), http.StatusAccepted)

		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword})
		wantStatus(t, r, http.StatusForbidden)
	})

	tests := []struct {
		name            string
		body            any
		wantErrContains []string
	}{
		{"invalid email", map[string]any{"email": "not-an-email", "password": testPassword}, []string{"email"}},
		{"email too long", map[string]any{"email": strings.Repeat("a", 250) + "@example.com", "password": testPassword}, []string{"email"}},
		{"missing email", map[string]any{"password": testPassword}, []string{"email"}},
		{"missing password", map[string]any{"email": testEmail}, []string{"password"}},
		{"password too short", map[string]any{"email": testEmail, "password": "1234567"}, []string{"password"}},
		{"password too long", map[string]any{"email": testEmail, "password": strings.Repeat("a", 129)}, []string{"password"}},
		{"malformed JSON", `{"email":`, []string{"malformed json"}},
		{"empty body", nil, []string{"body is required"}},
		{"wrong type", map[string]any{"email": 1, "password": testPassword}, []string{"email"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setupAuthTest(t)
			r := e.do(t, http.MethodPost, "/auth/register", tt.body)
			wantStatus(t, r, http.StatusBadRequest)
			checkErrorMesssage(t, r.errorMessage(t), tt.wantErrContains)
		})
	}

	t.Run("password of exactly 8 and 128 characters is accepted", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": "a@example.com", "password": strings.Repeat("a", 8)}), http.StatusAccepted)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": "b@example.com", "password": strings.Repeat("a", 128)}), http.StatusAccepted)
	})

	t.Run("rate limited per email", func(t *testing.T) {
		e := setupAuthTest(t)
		var got apiResp
		for i := range rateLimitProbes {
			// A different IP each time, so only the per-email limit can trigger.
			got = e.do(t, http.MethodPost, "/auth/register",
				map[string]any{"email": testEmail, "password": testPassword},
				withRemoteAddr("10.0.0."+strconv.Itoa(i%250+1)+":1234"))
			if got.Status == http.StatusTooManyRequests {
				break
			}
		}
		wantStatus(t, got, http.StatusTooManyRequests)
		wantRetryAfter(t, got)
	})

	t.Run("rate limited per IP", func(t *testing.T) {
		e := setupAuthTest(t)
		var got apiResp
		for i := range rateLimitProbes {
			got = e.do(t, http.MethodPost, "/auth/register",
				map[string]any{"email": "user" + strconv.Itoa(i) + "@example.com", "password": testPassword},
				withRemoteAddr("192.0.2.1:1234"))
			if got.Status == http.StatusTooManyRequests {
				break
			}
		}
		wantStatus(t, got, http.StatusTooManyRequests)
		wantRetryAfter(t, got)
	})
}

// --- POST /auth/verify-email -------------------------------------------------

func TestVerifyEmail(t *testing.T) {
	register := func(t *testing.T, e *authEnv) string {
		t.Helper()
		wantStatus(t, e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": testEmail, "password": testPassword}), http.StatusAccepted)
		return e.lastMail(t, testEmail, mailVerifyEmail).Token
	}

	t.Run("success: 204 and the account can sign in", func(t *testing.T) {
		e := setupAuthTest(t)
		token := register(t, e)

		r := e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": token})
		wantStatus(t, r, http.StatusNoContent)
		wantEmptyBody(t, r)

		e.loginToken(t, testEmail, testPassword)
	})

	t.Run("a token is single use", func(t *testing.T) {
		e := setupAuthTest(t)
		token := register(t, e)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": token}), http.StatusNoContent)

		r := e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": token})
		wantStatus(t, r, http.StatusBadRequest)
	})

	t.Run("unknown, expired and used tokens get the same 400", func(t *testing.T) {
		e := setupAuthTest(t)
		used := register(t, e)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": used}), http.StatusNoContent)

		// A second, never-verified account provides the token that expires.
		wantStatus(t, e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": "bob@example.com", "password": testPassword}), http.StatusAccepted)
		stale := e.lastMail(t, "bob@example.com", mailVerifyEmail).Token
		e.Advance(30 * 24 * time.Hour)

		var msgs []string
		for _, tok := range []string{"does-not-exist", used, stale} {
			r := e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": tok})
			wantStatus(t, r, http.StatusBadRequest)
			msgs = append(msgs, r.errorMessage(t))
		}
		if msgs[0] != msgs[1] || msgs[1] != msgs[2] {
			t.Errorf("error messages differ, they must not reveal why: %q", msgs)
		}
	})

	t.Run("a password-reset token cannot verify an email", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/forgot-password", map[string]any{"email": testEmail}), http.StatusAccepted)
		reset := e.lastMail(t, testEmail, mailResetPassword).Token

		wantStatus(t, e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": reset}), http.StatusBadRequest)
	})

	tests := []struct {
		name string
		body any
	}{
		{"missing token", map[string]any{}},
		{"empty token", map[string]any{"token": ""}},
		{"token too long", map[string]any{"token": strings.Repeat("a", 129)}},
		{"wrong type", map[string]any{"token": 1}},
		{"malformed JSON", `{"token":`},
		{"empty body", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setupAuthTest(t)
			r := e.do(t, http.MethodPost, "/auth/verify-email", tt.body)
			wantStatus(t, r, http.StatusBadRequest)
		})
	}

	t.Run("GET is not allowed", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodGet, "/auth/verify-email?token=x", nil), http.StatusMethodNotAllowed)
	})
}

// --- POST /auth/resend-verification ------------------------------------------

func TestResendVerification(t *testing.T) {
	t.Run("unverified account: 202 and a mail whose token works", func(t *testing.T) {
		e := setupAuthTest(t)
		e.AddUser(t, testEmail, testPassword, userState{})

		r := e.do(t, http.MethodPost, "/auth/resend-verification", map[string]any{"email": testEmail})
		wantStatus(t, r, http.StatusAccepted)
		wantEmptyBody(t, r)

		token := e.lastMail(t, testEmail, mailVerifyEmail).Token
		wantStatus(t, e.do(t, http.MethodPost, "/auth/verify-email", map[string]any{"token": token}), http.StatusNoContent)
	})

	t.Run("unknown and verified accounts: the same 202 and no verification mail", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		for _, email := range []string{testEmail, "nobody@example.com"} {
			r := e.do(t, http.MethodPost, "/auth/resend-verification", map[string]any{"email": email})
			wantStatus(t, r, http.StatusAccepted)
			wantEmptyBody(t, r)
			for _, m := range e.Mails(t, email) {
				if m.Kind == mailVerifyEmail {
					t.Errorf("%s got a verification mail", email)
				}
			}
		}
	})

	t.Run("email is case-insensitive", func(t *testing.T) {
		e := setupAuthTest(t)
		e.AddUser(t, testEmail, testPassword, userState{})
		wantStatus(t, e.do(t, http.MethodPost, "/auth/resend-verification", map[string]any{"email": "ALICE@EXAMPLE.COM"}), http.StatusAccepted)
		e.lastMail(t, testEmail, mailVerifyEmail)
	})

	for _, tt := range []struct {
		name string
		body any
	}{
		{"missing email", map[string]any{}},
		{"invalid email", map[string]any{"email": "nope"}},
		{"malformed JSON", `{`},
		{"empty body", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := setupAuthTest(t)
			wantStatus(t, e.do(t, http.MethodPost, "/auth/resend-verification", tt.body), http.StatusBadRequest)
		})
	}

	t.Run("rate limited per email", func(t *testing.T) {
		e := setupAuthTest(t)
		e.AddUser(t, testEmail, testPassword, userState{})
		var got apiResp
		for i := range rateLimitProbes {
			got = e.do(t, http.MethodPost, "/auth/resend-verification", map[string]any{"email": testEmail},
				withRemoteAddr("10.0.0."+strconv.Itoa(i%250+1)+":1234"))
			if got.Status == http.StatusTooManyRequests {
				break
			}
		}
		wantStatus(t, got, http.StatusTooManyRequests)
		wantRetryAfter(t, got)
	})
}

// --- POST /auth/login --------------------------------------------------------

func TestLogin(t *testing.T) {
	t.Run("token mode (default): refresh token in the body, no cookie", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword})
		wantStatus(t, r, http.StatusOK)

		tr := decodeTokens(t, r)
		if tr.AccessToken == "" {
			t.Error("access_token is empty")
		}
		if tr.TokenType != "Bearer" {
			t.Errorf("token_type = %q, want Bearer", tr.TokenType)
		}
		if tr.ExpiresIn < 1 {
			t.Errorf("expires_in = %d, want >= 1", tr.ExpiresIn)
		}
		if tr.RefreshToken == nil || *tr.RefreshToken == "" {
			t.Error("refresh_token missing in token mode")
		}
		if c := r.cookie("refresh_token"); c != nil {
			t.Errorf("unexpected refresh_token cookie in token mode: %+v", c)
		}
		if got := e.meStatus(t, tr.AccessToken); got != http.StatusOK {
			t.Errorf("GET /me with the new access token = %d, want 200", got)
		}
	})

	t.Run("explicit token_mode token behaves like the default", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword, "token_mode": "token"})
		wantStatus(t, r, http.StatusOK)
		if decodeTokens(t, r).RefreshToken == nil {
			t.Error("refresh_token missing")
		}
	})

	t.Run("cookie mode: HttpOnly Secure cookie under /auth, not in the body", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword, "token_mode": "cookie"})
		wantStatus(t, r, http.StatusOK)

		tr := decodeTokens(t, r)
		if tr.AccessToken == "" {
			t.Error("access_token is empty (it is always in the body)")
		}
		if tr.RefreshToken != nil {
			t.Error("refresh_token must not be in the body in cookie mode")
		}
		c := r.cookie("refresh_token")
		if c == nil || c.Value == "" {
			t.Fatal("refresh_token cookie not set")
		}
		if !c.HttpOnly || !c.Secure || c.Path != "/auth" {
			t.Errorf("cookie = %+v, want HttpOnly, Secure and Path=/auth", c)
		}
	})

	t.Run("email is case-insensitive", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		e.loginToken(t, "Alice@EXAMPLE.com", testPassword)
	})

	t.Run("401: unknown email and wrong password give the same message", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		unknown := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": "nobody@example.com", "password": testPassword})
		wrong := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": "wrong password"})
		wantStatus(t, unknown, http.StatusUnauthorized)
		wantStatus(t, wrong, http.StatusUnauthorized)

		checkErrorMesssage(t, unknown.errorMessage(t), []string{"invalid email or password"})
		if unknown.errorMessage(t) != wrong.errorMessage(t) {
			t.Errorf("messages differ: %q vs %q", unknown.errorMessage(t), wrong.errorMessage(t))
		}
	})

	t.Run("403: right password but email not verified", func(t *testing.T) {
		e := setupAuthTest(t)
		e.AddUser(t, testEmail, testPassword, userState{})
		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword})
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"email not verified"})
	})

	t.Run("403: right password but account disabled", func(t *testing.T) {
		e := setupAuthTest(t)
		e.AddUser(t, testEmail, testPassword, userState{Verified: true, Disabled: true})
		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword})
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"account disabled"})
	})

	t.Run("wrong password never reveals unverified or disabled state", func(t *testing.T) {
		e := setupAuthTest(t)
		e.AddUser(t, "unverified@example.com", testPassword, userState{})
		e.AddUser(t, "disabled@example.com", testPassword, userState{Verified: true, Disabled: true})
		for _, email := range []string{"unverified@example.com", "disabled@example.com"} {
			r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": email, "password": "wrong password"})
			wantStatus(t, r, http.StatusUnauthorized)
		}
	})

	validationTests := []struct {
		name string
		body any
	}{
		{"missing email", map[string]any{"password": testPassword}},
		{"missing password", map[string]any{"email": testEmail}},
		{"invalid email", map[string]any{"email": "nope", "password": testPassword}},
		{"empty password", map[string]any{"email": testEmail, "password": ""}},
		{"password too long", map[string]any{"email": testEmail, "password": strings.Repeat("a", 129)}},
		// "ﬁ" (U+FB01) is one rune but becomes "fi" under NFKC: 65 runes -> 130.
		{"password too long after NFKC", map[string]any{"email": testEmail, "password": strings.Repeat("\ufb01", 65)}},
		{"invalid token_mode", map[string]any{"email": testEmail, "password": testPassword, "token_mode": "session"}},
		{"malformed JSON", `{"email":`},
		{"empty body", nil},
	}
	for _, tt := range validationTests {
		t.Run("400: "+tt.name, func(t *testing.T) {
			e := setupAuthTest(t)
			e.verifiedUser(t, testEmail)
			wantStatus(t, e.do(t, http.MethodPost, "/auth/login", tt.body), http.StatusBadRequest)
		})
	}

	// Only empty and too-long passwords are 400. Everything else, including a
	// password shorter than the 8-character minimum of register, is a wrong
	// password and gets the same 401 as any other.
	t.Run("401: a password shorter than the registration minimum", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		for _, pw := range []string{"a", "1234567"} {
			r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": pw})
			wantStatus(t, r, http.StatusUnauthorized)
			checkErrorMesssage(t, r.errorMessage(t), []string{"invalid email or password"})
		}
	})

	t.Run("401: a short password for an unknown email looks the same", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": "nobody@example.com", "password": "short"})
		wantStatus(t, r, http.StatusUnauthorized)
		checkErrorMesssage(t, r.errorMessage(t), []string{"invalid email or password"})
	})

	t.Run("401: a password of exactly 128 characters is not rejected as too long", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": strings.Repeat("a", 128)}), http.StatusUnauthorized)
	})

	t.Run("length is counted after NFKC: 140 runes that normalize to 70 are a 401, not a 400", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		decomposed := strings.Repeat("e\u0301", 70) // 'e' + combining acute, composes to one rune
		wantStatus(t, e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": decomposed}), http.StatusUnauthorized)
	})

	t.Run("a rejected password does not count as a failed attempt", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		for range 10 { // more than the fake limiter's free attempts
			wantStatus(t, e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": ""}), http.StatusBadRequest)
		}
		e.loginToken(t, testEmail, testPassword)
	})

	t.Run("400 comes before 429", func(t *testing.T) {
		e := setupAuthTest(t)
		exhaustLoginLimit(t, e, testEmail)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail}), http.StatusBadRequest)
	})

	t.Run("429 per email: even the right password is refused while waiting", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		last := exhaustLoginLimit(t, e, testEmail)
		wantRetryAfter(t, last)

		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword},
			withRemoteAddr("198.51.100.77:1234"))
		wantStatus(t, r, http.StatusTooManyRequests)
		wantRetryAfter(t, r)
	})

	t.Run("429 per IP", func(t *testing.T) {
		e := setupAuthTest(t)
		var got apiResp
		for i := range rateLimitProbes {
			got = e.do(t, http.MethodPost, "/auth/login",
				map[string]any{"email": "user" + strconv.Itoa(i) + "@example.com", "password": "x"},
				withRemoteAddr("192.0.2.9:1234"))
			if got.Status == http.StatusTooManyRequests {
				break
			}
		}
		wantStatus(t, got, http.StatusTooManyRequests)
		wantRetryAfter(t, got)
	})

	t.Run("429 is checked before the password: no 401/403 leak while limited", func(t *testing.T) {
		e := setupAuthTest(t)
		e.AddUser(t, testEmail, testPassword, userState{}) // unverified

		exhaustLoginLimit(t, e, testEmail)
		r := e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword},
			withRemoteAddr("198.51.100.78:1234"))
		wantStatus(t, r, http.StatusTooManyRequests)
	})
}

// exhaustLoginLimit sends wrong-password logins for email, each from another
// IP, until the per-email limit answers 429, and returns that answer.
func exhaustLoginLimit(tb testing.TB, e *authEnv, email string) apiResp {
	tb.Helper()
	var got apiResp
	for i := range rateLimitProbes {
		got = e.do(tb, http.MethodPost, "/auth/login",
			map[string]any{"email": email, "password": "wrong password"},
			withRemoteAddr("10.1.0."+strconv.Itoa(i%250+1)+":1234"))
		if got.Status == http.StatusTooManyRequests {
			return got
		}
	}
	tb.Fatalf("no 429 after %d wrong logins, last status %d", rateLimitProbes, got.Status)
	return got
}

// --- POST /auth/refresh ------------------------------------------------------

const refreshGrace = 15 * time.Second

func TestRefresh(t *testing.T) {
	t.Run("body token: new access and refresh token in the body, no cookie", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)

		r := e.refreshWithBody(t, *first.RefreshToken)
		wantStatus(t, r, http.StatusOK)
		next := decodeTokens(t, r)

		if next.AccessToken == "" || next.TokenType != "Bearer" || next.ExpiresIn < 1 {
			t.Errorf("token response = %+v", next)
		}
		if next.RefreshToken == nil || *next.RefreshToken == "" || *next.RefreshToken == *first.RefreshToken {
			t.Errorf("refresh_token must be a new token, got %v", next.RefreshToken)
		}
		if c := r.cookie("refresh_token"); c != nil {
			t.Errorf("unexpected cookie for a body token: %+v", c)
		}
		if got := e.meStatus(t, next.AccessToken); got != http.StatusOK {
			t.Errorf("GET /me with refreshed access token = %d, want 200", got)
		}
	})

	t.Run("cookie token: replaced by a new cookie, not in the body", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, old := e.loginCookie(t, testEmail, testPassword)

		r := e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(old))
		wantStatus(t, r, http.StatusOK)

		tr := decodeTokens(t, r)
		if tr.AccessToken == "" {
			t.Error("access_token is empty")
		}
		if tr.RefreshToken != nil {
			t.Error("refresh_token must not be in the body for a cookie token")
		}
		c := r.cookie("refresh_token")
		if c == nil || c.Value == "" || c.Value == old {
			t.Fatalf("want a new refresh_token cookie, got %+v", c)
		}
		if !c.HttpOnly || !c.Secure || c.Path != "/auth" {
			t.Errorf("cookie = %+v, want HttpOnly, Secure and Path=/auth", c)
		}
	})

	t.Run("cookie with an empty JSON object body", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, old := e.loginCookie(t, testEmail, testPassword)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", map[string]any{}, withRefreshCookie(old)), http.StatusOK)
	})

	t.Run("the body token wins over the cookie only when the body has one", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		bodyLogin := e.loginToken(t, testEmail, testPassword)
		_, cookie := e.loginCookie(t, testEmail, testPassword)

		r := e.do(t, http.MethodPost, "/auth/refresh", map[string]any{"refresh_token": *bodyLogin.RefreshToken}, withRefreshCookie(cookie))
		wantStatus(t, r, http.StatusOK)
		if decodeTokens(t, r).RefreshToken == nil {
			t.Error("answer should follow the body token and return refresh_token in the body")
		}
	})

	t.Run("old token inside the grace period: new access token only", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)
		wantStatus(t, e.refreshWithBody(t, *first.RefreshToken), http.StatusOK)

		e.Advance(refreshGrace - time.Second)
		r := e.refreshWithBody(t, *first.RefreshToken)
		wantStatus(t, r, http.StatusOK)

		tr := decodeTokens(t, r)
		if tr.AccessToken == "" {
			t.Error("access_token is empty")
		}
		if tr.RefreshToken != nil {
			t.Error("a grace-period answer must not contain refresh_token")
		}
		if got := e.meStatus(t, tr.AccessToken); got != http.StatusOK {
			t.Errorf("GET /me with grace-period access token = %d, want 200", got)
		}
	})

	t.Run("old cookie token inside the grace period: no new cookie", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, old := e.loginCookie(t, testEmail, testPassword)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(old)), http.StatusOK)

		r := e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(old))
		wantStatus(t, r, http.StatusOK)
		if c := r.cookie("refresh_token"); c != nil {
			t.Errorf("grace-period answer must not set a cookie, got %+v", c)
		}
		if decodeTokens(t, r).RefreshToken != nil {
			t.Error("grace-period answer must not contain refresh_token")
		}
	})

	t.Run("old token after the grace period: 401 and the whole session is revoked", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)

		r := e.refreshWithBody(t, *first.RefreshToken)
		wantStatus(t, r, http.StatusOK)
		second := decodeTokens(t, r)

		e.Advance(refreshGrace + time.Second)
		reuse := e.refreshWithBody(t, *first.RefreshToken)
		wantStatus(t, reuse, http.StatusUnauthorized)
		checkErrorMesssage(t, reuse.errorMessage(t), []string{"unauthorized"})

		// Reuse is treated as theft: the newest token and its access token die too.
		wantStatus(t, e.refreshWithBody(t, *second.RefreshToken), http.StatusUnauthorized)
		if got := e.meStatus(t, second.AccessToken); got != http.StatusUnauthorized {
			t.Errorf("GET /me after session revocation = %d, want 401", got)
		}
	})

	t.Run("the refresh token keeps the expiry of the session", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, old := e.loginCookie(t, testEmail, testPassword)

		first := e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(old))
		wantStatus(t, first, http.StatusOK)
		c1 := first.cookie("refresh_token")

		e.Advance(time.Hour)
		second := e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(c1.Value))
		wantStatus(t, second, http.StatusOK)
		c2 := second.cookie("refresh_token")

		if !c1.Expires.Equal(c2.Expires) {
			t.Errorf("cookie expiry moved on rotation: %v -> %v", c1.Expires, c2.Expires)
		}
	})

	t.Run("a rotated token can be used again", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		cur := *e.loginToken(t, testEmail, testPassword).RefreshToken

		for range 3 {
			e.Advance(refreshGrace + time.Second)
			r := e.refreshWithBody(t, cur)
			wantStatus(t, r, http.StatusOK)
			cur = *decodeTokens(t, r).RefreshToken
		}
	})

	t.Run("a disabled account cannot refresh inside the grace period either", func(t *testing.T) {
		e := setupAuthTest(t)
		u := e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)
		wantStatus(t, e.refreshWithBody(t, *first.RefreshToken), http.StatusOK)

		e.Disable(t, u.ID)
		r := e.refreshWithBody(t, *first.RefreshToken)
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"account disabled"})
	})

	t.Run("the session survives a grace-period request", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)
		second := decodeTokens(t, e.refreshWithBody(t, *first.RefreshToken))
		wantStatus(t, e.refreshWithBody(t, *first.RefreshToken), http.StatusOK)

		wantStatus(t, e.refreshWithBody(t, *second.RefreshToken), http.StatusOK)
	})

	t.Run("401 and cleared cookie for an unknown cookie token", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie("unknown"))
		wantStatus(t, r, http.StatusUnauthorized)
		wantClearedCookie(t, r)
		checkErrorMesssage(t, r.errorMessage(t), []string{"unauthorized"})
	})

	t.Run("401 for an unknown body token", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.refreshWithBody(t, "unknown")
		wantStatus(t, r, http.StatusUnauthorized)
		checkErrorMesssage(t, r.errorMessage(t), []string{"unauthorized"})
	})

	t.Run("401 when no token is sent at all", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil), http.StatusUnauthorized)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", map[string]any{}), http.StatusUnauthorized)
	})

	t.Run("401 for an expired session", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)
		e.Advance(31 * 24 * time.Hour)
		wantStatus(t, e.refreshWithBody(t, *first.RefreshToken), http.StatusUnauthorized)
	})

	t.Run("an access token is not a refresh token", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)
		wantStatus(t, e.refreshWithBody(t, first.AccessToken), http.StatusUnauthorized)
	})

	// The length of the token is not validated by the API: a token that is too
	// long can never be a real one, so it is just an unknown token.
	t.Run("401 for a body token longer than 128 characters", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.refreshWithBody(t, strings.Repeat("a", 129))
		wantStatus(t, r, http.StatusUnauthorized)
		checkErrorMesssage(t, r.errorMessage(t), []string{"unauthorized"})
	})

	t.Run("400 for malformed JSON", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", `{"refresh_token":`), http.StatusBadRequest)
	})

	t.Run("cookie mode: a foreign Origin is 403", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, cookie := e.loginCookie(t, testEmail, testPassword)

		r := e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(cookie), withOrigin("https://evil.example"))
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"forbidden"})

		// The rejected request must not have used up the token.
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(cookie)), http.StatusOK)
	})

	t.Run("cookie mode: an allowed Origin or none is accepted", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, c1 := e.loginCookie(t, testEmail, testPassword)
		_, c2 := e.loginCookie(t, testEmail, testPassword)

		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(c1), withOrigin(e.AllowedOrigin)), http.StatusOK)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(c2)), http.StatusOK)
	})

	t.Run("body mode ignores the Origin header", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)
		r := e.do(t, http.MethodPost, "/auth/refresh", map[string]any{"refresh_token": *first.RefreshToken}, withOrigin("https://evil.example"))
		wantStatus(t, r, http.StatusOK)
	})

	t.Run("a disabled account cannot refresh", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		u := e.verifiedUser(t, "carol@example.com")
		carol := e.loginToken(t, "carol@example.com", testPassword)
		e.Disable(t, u.ID)

		r := e.refreshWithBody(t, *carol.RefreshToken)
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"account disabled"})

		// Other users are not affected.
		alice := e.loginToken(t, testEmail, testPassword)
		wantStatus(t, e.refreshWithBody(t, *alice.RefreshToken), http.StatusOK)
	})
}

// --- POST /auth/logout -------------------------------------------------------

func TestLogout(t *testing.T) {
	t.Run("body token: 204, session and access token revoked", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		r := e.do(t, http.MethodPost, "/auth/logout", map[string]any{"refresh_token": *tr.RefreshToken})
		wantStatus(t, r, http.StatusNoContent)
		wantEmptyBody(t, r)

		wantStatus(t, e.refreshWithBody(t, *tr.RefreshToken), http.StatusUnauthorized)
		if got := e.meStatus(t, tr.AccessToken); got != http.StatusUnauthorized {
			t.Errorf("GET /me after logout = %d, want 401", got)
		}
	})

	t.Run("cookie token: 204 and the cookie is cleared", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr, cookie := e.loginCookie(t, testEmail, testPassword)

		r := e.do(t, http.MethodPost, "/auth/logout", nil, withRefreshCookie(cookie))
		wantStatus(t, r, http.StatusNoContent)
		wantClearedCookie(t, r)

		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(cookie)), http.StatusUnauthorized)
		if got := e.meStatus(t, tr.AccessToken); got != http.StatusUnauthorized {
			t.Errorf("GET /me after logout = %d, want 401", got)
		}
	})

	t.Run("works after the access token has expired and needs no Authorization", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		e.Advance(24 * time.Hour)

		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", map[string]any{"refresh_token": *tr.RefreshToken}), http.StatusNoContent)
	})

	t.Run("unknown token, repeated logout and no token are all 204", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		body := map[string]any{"refresh_token": *tr.RefreshToken}

		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", body), http.StatusNoContent)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", body), http.StatusNoContent)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", map[string]any{"refresh_token": "unknown"}), http.StatusNoContent)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", nil), http.StatusNoContent)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", map[string]any{}), http.StatusNoContent)
	})

	t.Run("only this session is revoked", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		a := e.loginToken(t, testEmail, testPassword)
		b := e.loginToken(t, testEmail, testPassword)

		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", map[string]any{"refresh_token": *a.RefreshToken}), http.StatusNoContent)

		if got := e.meStatus(t, b.AccessToken); got != http.StatusOK {
			t.Errorf("other session: GET /me = %d, want 200", got)
		}
		wantStatus(t, e.refreshWithBody(t, *b.RefreshToken), http.StatusOK)
	})

	// Logout never reports a bad token: the client wants to be signed out, and
	// the cookie is cleared whatever the token was.
	t.Run("an invalid cookie token is 204 and the cookie is still cleared", func(t *testing.T) {
		e := setupAuthTest(t)
		for name, token := range map[string]string{
			"unknown":      "unknown",
			"too long":     strings.Repeat("a", 129),
			"access token": "eyJhbGciOiJFZERTQSJ9.e30.sig",
		} {
			t.Run(name, func(t *testing.T) {
				r := e.do(t, http.MethodPost, "/auth/logout", nil, withRefreshCookie(token))
				wantStatus(t, r, http.StatusNoContent)
				wantEmptyBody(t, r)
				wantClearedCookie(t, r)
			})
		}
	})

	t.Run("an already revoked cookie token is 204 and the cookie is cleared", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, cookie := e.loginCookie(t, testEmail, testPassword)

		for range 2 {
			r := e.do(t, http.MethodPost, "/auth/logout", nil, withRefreshCookie(cookie))
			wantStatus(t, r, http.StatusNoContent)
			wantClearedCookie(t, r)
		}
	})

	t.Run("no token at all: 204 and the cookie is cleared", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.do(t, http.MethodPost, "/auth/logout", nil)
		wantStatus(t, r, http.StatusNoContent)
		wantClearedCookie(t, r)
	})

	t.Run("a body token longer than 128 characters is 204", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.do(t, http.MethodPost, "/auth/logout", map[string]any{"refresh_token": strings.Repeat("a", 129)})
		wantStatus(t, r, http.StatusNoContent)
		wantEmptyBody(t, r)
	})

	t.Run("400 for malformed JSON", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", `{"refresh_token":`), http.StatusBadRequest)
	})

	t.Run("cookie mode: a foreign Origin is 403 and nothing is revoked", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, cookie := e.loginCookie(t, testEmail, testPassword)

		r := e.do(t, http.MethodPost, "/auth/logout", nil, withRefreshCookie(cookie), withOrigin("https://evil.example"))
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"forbidden"})

		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(cookie)), http.StatusOK)
	})

	t.Run("cookie mode: an allowed Origin is accepted", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		_, cookie := e.loginCookie(t, testEmail, testPassword)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", nil, withRefreshCookie(cookie), withOrigin(e.AllowedOrigin)), http.StatusNoContent)
	})
}

// --- POST /auth/logout-all ---------------------------------------------------

func TestLogoutAll(t *testing.T) {
	t.Run("revokes every session and access token of the user, clears the cookie", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		a := e.loginToken(t, testEmail, testPassword)
		b, bCookie := e.loginCookie(t, testEmail, testPassword)

		// Revocation has one-second granularity: tokens issued in the same second
		// as the logout are not revoked.
		e.Advance(time.Second)
		r := e.do(t, http.MethodPost, "/auth/logout-all", nil, withBearer(a.AccessToken))
		wantStatus(t, r, http.StatusNoContent)
		wantEmptyBody(t, r)
		wantClearedCookie(t, r)

		for name, tok := range map[string]string{"a": a.AccessToken, "b": b.AccessToken} {
			if got := e.meStatus(t, tok); got != http.StatusUnauthorized {
				t.Errorf("session %s: GET /me = %d, want 401", name, got)
			}
		}
		wantStatus(t, e.refreshWithBody(t, *a.RefreshToken), http.StatusUnauthorized)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(bCookie)), http.StatusUnauthorized)
	})

	t.Run("does not touch other users", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		e.verifiedUser(t, "bob@example.com")
		alice := e.loginToken(t, testEmail, testPassword)
		bob := e.loginToken(t, "bob@example.com", testPassword)

		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout-all", nil, withBearer(alice.AccessToken)), http.StatusNoContent)

		if got := e.meStatus(t, bob.AccessToken); got != http.StatusOK {
			t.Errorf("bob: GET /me = %d, want 200", got)
		}
		wantStatus(t, e.refreshWithBody(t, *bob.RefreshToken), http.StatusOK)
	})

	t.Run("the user can sign in again afterwards", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout-all", nil, withBearer(tr.AccessToken)), http.StatusNoContent)

		again := e.loginToken(t, testEmail, testPassword)
		if got := e.meStatus(t, again.AccessToken); got != http.StatusOK {
			t.Errorf("GET /me with a new token = %d, want 200", got)
		}
	})

	t.Run("401 without a valid access token", func(t *testing.T) {
		e := setupAuthTest(t)
		u := e.verifiedUser(t, testEmail)

		for name, opts := range map[string][]reqOpt{
			"no header":   nil,
			"garbage":     {withBearer("garbage")},
			"expired":     {withBearer(e.ForgeToken(t, u.ID, "expired"))},
			"wrong key":   {withBearer(e.ForgeToken(t, u.ID, "wrong_key"))},
			"basic auth":  {withHeader("Authorization", "Basic dXNlcjpwYXNz")},
			"empty token": {withHeader("Authorization", "Bearer ")},
		} {
			t.Run(name, func(t *testing.T) {
				r := e.do(t, http.MethodPost, "/auth/logout-all", nil, opts...)
				wantStatus(t, r, http.StatusUnauthorized)
				if got := r.Header.Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				checkErrorMesssage(t, r.errorMessage(t), []string{"unauthorized"})
			})
		}
	})
}

// --- POST /auth/forgot-password ----------------------------------------------

func TestForgotPassword(t *testing.T) {
	t.Run("known email: 202, empty body, reset mail", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		r := e.do(t, http.MethodPost, "/auth/forgot-password", map[string]any{"email": testEmail})
		wantStatus(t, r, http.StatusAccepted)
		wantEmptyBody(t, r)
		if e.lastMail(t, testEmail, mailResetPassword).Token == "" {
			t.Error("reset mail carries no token")
		}
	})

	t.Run("unknown email: the same 202 and no mail", func(t *testing.T) {
		e := setupAuthTest(t)
		r := e.do(t, http.MethodPost, "/auth/forgot-password", map[string]any{"email": "nobody@example.com"})
		wantStatus(t, r, http.StatusAccepted)
		wantEmptyBody(t, r)
		if mails := e.Mails(t, "nobody@example.com"); len(mails) != 0 {
			t.Errorf("mails to an unknown address = %+v, want none", mails)
		}
	})

	t.Run("email is case-insensitive", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/forgot-password", map[string]any{"email": "ALICE@example.com"}), http.StatusAccepted)
		e.lastMail(t, testEmail, mailResetPassword)
	})

	for _, tt := range []struct {
		name string
		body any
	}{
		{"missing email", map[string]any{}},
		{"invalid email", map[string]any{"email": "nope"}},
		{"malformed JSON", `{`},
		{"empty body", nil},
	} {
		t.Run("400: "+tt.name, func(t *testing.T) {
			e := setupAuthTest(t)
			wantStatus(t, e.do(t, http.MethodPost, "/auth/forgot-password", tt.body), http.StatusBadRequest)
		})
	}

	t.Run("rate limited per email", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		var got apiResp
		for i := range rateLimitProbes {
			got = e.do(t, http.MethodPost, "/auth/forgot-password", map[string]any{"email": testEmail},
				withRemoteAddr("10.2.0."+strconv.Itoa(i%250+1)+":1234"))
			if got.Status == http.StatusTooManyRequests {
				break
			}
		}
		wantStatus(t, got, http.StatusTooManyRequests)
		wantRetryAfter(t, got)
	})

	t.Run("rate limited per IP", func(t *testing.T) {
		e := setupAuthTest(t)
		var got apiResp
		for i := range rateLimitProbes {
			got = e.do(t, http.MethodPost, "/auth/forgot-password",
				map[string]any{"email": "user" + strconv.Itoa(i) + "@example.com"},
				withRemoteAddr("192.0.2.33:1234"))
			if got.Status == http.StatusTooManyRequests {
				break
			}
		}
		wantStatus(t, got, http.StatusTooManyRequests)
		wantRetryAfter(t, got)
	})
}

// --- POST /auth/reset-password -----------------------------------------------

func TestResetPassword(t *testing.T) {
	resetToken := func(t *testing.T, e *authEnv) string {
		t.Helper()
		wantStatus(t, e.do(t, http.MethodPost, "/auth/forgot-password", map[string]any{"email": testEmail}), http.StatusAccepted)
		return e.lastMail(t, testEmail, mailResetPassword).Token
	}

	t.Run("success: 204, new password works, old one does not", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		token := resetToken(t, e)

		r := e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": token, "new_password": testNewPassword})
		wantStatus(t, r, http.StatusNoContent)
		wantEmptyBody(t, r)

		e.loginToken(t, testEmail, testNewPassword)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword}), http.StatusUnauthorized)
	})

	t.Run("every session of the user is revoked", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		a := e.loginToken(t, testEmail, testPassword)
		b, bCookie := e.loginCookie(t, testEmail, testPassword)
		token := resetToken(t, e)

		// Revocation has one-second granularity: tokens issued in the same second
		// as the reset are not revoked.
		e.Advance(time.Second)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": token, "new_password": testNewPassword}), http.StatusNoContent)

		for name, tok := range map[string]string{"a": a.AccessToken, "b": b.AccessToken} {
			if got := e.meStatus(t, tok); got != http.StatusUnauthorized {
				t.Errorf("session %s: GET /me = %d, want 401", name, got)
			}
		}
		wantStatus(t, e.refreshWithBody(t, *a.RefreshToken), http.StatusUnauthorized)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(bCookie)), http.StatusUnauthorized)
	})

	t.Run("a token is single use", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		token := resetToken(t, e)
		body := map[string]any{"token": token, "new_password": testNewPassword}
		wantStatus(t, e.do(t, http.MethodPost, "/auth/reset-password", body), http.StatusNoContent)

		wantStatus(t, e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": token, "new_password": "yet another password"}), http.StatusBadRequest)
		e.loginToken(t, testEmail, testNewPassword)
	})

	t.Run("unknown, expired and used tokens get the same 400", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)

		used := resetToken(t, e)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": used, "new_password": testNewPassword}), http.StatusNoContent)
		stale := resetToken(t, e)
		e.Advance(24 * time.Hour)

		var msgs []string
		for _, tok := range []string{"does-not-exist", used, stale} {
			r := e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": tok, "new_password": testNewPassword})
			wantStatus(t, r, http.StatusBadRequest)
			msgs = append(msgs, r.errorMessage(t))
		}
		if msgs[0] != msgs[1] || msgs[1] != msgs[2] {
			t.Errorf("error messages differ, they must not reveal why: %q", msgs)
		}
	})

	t.Run("an email-verification token cannot reset a password", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/register", map[string]any{"email": testEmail, "password": testPassword}), http.StatusAccepted)
		verify := e.lastMail(t, testEmail, mailVerifyEmail).Token

		r := e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": verify, "new_password": testNewPassword})
		wantStatus(t, r, http.StatusBadRequest)
	})

	t.Run("an invalid new password does not use up the token", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		token := resetToken(t, e)

		r := e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": token, "new_password": "short"})
		wantStatus(t, r, http.StatusBadRequest)
		checkErrorMesssage(t, r.errorMessage(t), []string{"password"})

		wantStatus(t, e.do(t, http.MethodPost, "/auth/reset-password", map[string]any{"token": token, "new_password": testNewPassword}), http.StatusNoContent)
	})

	for _, tt := range []struct {
		name string
		body any
	}{
		{"missing token", map[string]any{"new_password": testNewPassword}},
		{"empty token", map[string]any{"token": "", "new_password": testNewPassword}},
		{"token too long", map[string]any{"token": strings.Repeat("a", 129), "new_password": testNewPassword}},
		{"missing new_password", map[string]any{"token": "abc"}},
		{"new_password too short", map[string]any{"token": "abc", "new_password": "1234567"}},
		{"new_password too long", map[string]any{"token": "abc", "new_password": strings.Repeat("a", 129)}},
		{"malformed JSON", `{"token":`},
		{"empty body", nil},
	} {
		t.Run("400: "+tt.name, func(t *testing.T) {
			e := setupAuthTest(t)
			wantStatus(t, e.do(t, http.MethodPost, "/auth/reset-password", tt.body), http.StatusBadRequest)
		})
	}
}
