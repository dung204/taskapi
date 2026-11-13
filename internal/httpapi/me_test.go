package httpapi

// Black-box tests for the "me" group of openapi.yml, plus the rule that the
// task endpoints need a bearer token. They share setupAuthTest and the helpers
// in auth_test.go.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"uuid"
)

type meResp struct {
	ID            uuid.UUID `json:"id"`
	Email         string    `json:"email"`
	EmailVerified bool      `json:"email_verified"`
	CreatedAt     time.Time `json:"created_at"`
}

func wantUnauthorized(tb testing.TB, r apiResp) {
	tb.Helper()
	wantStatus(tb, r, http.StatusUnauthorized)
	if got := r.Header.Get("WWW-Authenticate"); got != "Bearer" {
		tb.Errorf("WWW-Authenticate = %q, want Bearer", got)
	}
	checkErrorMesssage(tb, r.errorMessage(tb), []string{"unauthorized"})
}

// badAuth lists Authorization setups that must all be answered with the same 401.
func badAuth(t *testing.T, e *authEnv, userID uuid.UUID) map[string][]reqOpt {
	t.Helper()
	return map[string][]reqOpt{
		"no header":      nil,
		"garbage token":  {withBearer("garbage")},
		"empty token":    {withHeader("Authorization", "Bearer ")},
		"wrong scheme":   {withHeader("Authorization", "Basic dXNlcjpwYXNz")},
		"no scheme":      {withHeader("Authorization", "abc.def.ghi")},
		"expired":        {withBearer(e.ForgeToken(t, userID, "expired"))},
		"wrong key":      {withBearer(e.ForgeToken(t, userID, "wrong_key"))},
		"refresh token":  nil, // replaced below once a real refresh token exists
		"truncated jwt":  {withBearer("eyJhbGciOiJFZERTQSJ9.e30")},
		"lowercase none": {withHeader("Authorization", "bearer ")},
	}
}

// --- GET /me -----------------------------------------------------------------

func TestGetMe(t *testing.T) {
	t.Run("returns the signed-in user", func(t *testing.T) {
		e := setupAuthTest(t)
		u := e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		r := e.do(t, http.MethodGet, "/me", nil, withBearer(tr.AccessToken))
		wantStatus(t, r, http.StatusOK)

		var got meResp
		if err := json.Unmarshal(r.Body, &got); err != nil {
			t.Fatalf("decode %q: %v", r.Body, err)
		}
		if got.ID != u.ID {
			t.Errorf("id = %s, want %s", got.ID, u.ID)
		}
		if got.Email != testEmail {
			t.Errorf("email = %q, want %q", got.Email, testEmail)
		}
		if !got.EmailVerified {
			t.Error("email_verified = false, want true")
		}
		if !got.CreatedAt.Equal(u.CreatedAt) {
			t.Errorf("created_at = %v, want %v", got.CreatedAt, u.CreatedAt)
		}
	})

	t.Run("exposes only id, email, email_verified and created_at", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		r := e.do(t, http.MethodGet, "/me", nil, withBearer(tr.AccessToken))
		wantStatus(t, r, http.StatusOK)

		var fields map[string]any
		if err := json.Unmarshal(r.Body, &fields); err != nil {
			t.Fatalf("decode %q: %v", r.Body, err)
		}
		for _, want := range []string{"id", "email", "email_verified", "created_at"} {
			if _, ok := fields[want]; !ok {
				t.Errorf("field %q is missing", want)
			}
		}
		for k := range fields {
			switch k {
			case "id", "email", "email_verified", "created_at":
			default:
				t.Errorf("unexpected field %q in /me response", k)
			}
		}
		if strings.Contains(strings.ToLower(string(r.Body)), "password") {
			t.Error("response mentions a password")
		}
	})

	t.Run("each user sees their own data", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		e.verifiedUser(t, "bob@example.com")
		bob := e.loginToken(t, "bob@example.com", testPassword)

		r := e.do(t, http.MethodGet, "/me", nil, withBearer(bob.AccessToken))
		wantStatus(t, r, http.StatusOK)
		var got meResp
		if err := json.Unmarshal(r.Body, &got); err != nil {
			t.Fatal(err)
		}
		if got.Email != "bob@example.com" {
			t.Errorf("email = %q, want bob@example.com", got.Email)
		}
	})

	t.Run("401 for every kind of bad token, with the same message", func(t *testing.T) {
		e := setupAuthTest(t)
		u := e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		cases := badAuth(t, e, u.ID)
		cases["refresh token"] = []reqOpt{withBearer(*tr.RefreshToken)}

		for name, opts := range cases {
			t.Run(name, func(t *testing.T) {
				wantUnauthorized(t, e.do(t, http.MethodGet, "/me", nil, opts...))
			})
		}
	})

	t.Run("401 once the access token has expired", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		e.Advance(time.Duration(tr.ExpiresIn)*time.Second + time.Minute)
		wantUnauthorized(t, e.do(t, http.MethodGet, "/me", nil, withBearer(tr.AccessToken)))
	})

	t.Run("a refreshed access token works", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		first := e.loginToken(t, testEmail, testPassword)
		e.Advance(time.Duration(first.ExpiresIn)*time.Second + time.Minute)

		next := decodeTokens(t, e.refreshWithBody(t, *first.RefreshToken))
		wantStatus(t, e.do(t, http.MethodGet, "/me", nil, withBearer(next.AccessToken)), http.StatusOK)
	})

	t.Run("401 after logout", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/logout", map[string]any{"refresh_token": *tr.RefreshToken}), http.StatusNoContent)

		wantUnauthorized(t, e.do(t, http.MethodGet, "/me", nil, withBearer(tr.AccessToken)))
	})

	t.Run("403 when the account is disabled", func(t *testing.T) {
		e := setupAuthTest(t)
		u := e.verifiedUser(t, testEmail)
		e.verifiedUser(t, "carol@example.com")
		tr := e.loginToken(t, testEmail, testPassword)
		carol := e.loginToken(t, "carol@example.com", testPassword)
		e.Disable(t, u.ID)

		r := e.do(t, http.MethodGet, "/me", nil, withBearer(tr.AccessToken))
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"account disabled"})
		if got := r.Header.Get("WWW-Authenticate"); got != "" {
			t.Errorf("WWW-Authenticate = %q, want none on a 403", got)
		}

		// Other users are not affected.
		wantStatus(t, e.do(t, http.MethodGet, "/me", nil, withBearer(carol.AccessToken)), http.StatusOK)
	})

	t.Run("POST is not allowed", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodPost, "/me", nil), http.StatusMethodNotAllowed)
	})
}

// --- PUT /me/password --------------------------------------------------------

func TestChangePassword(t *testing.T) {
	body := func(current, next string) map[string]any {
		return map[string]any{"current_password": current, "new_password": next}
	}

	t.Run("success: 204, new password works, old one does not", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		r := e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(tr.AccessToken))
		wantStatus(t, r, http.StatusNoContent)
		wantEmptyBody(t, r)

		e.loginToken(t, testEmail, testNewPassword)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": testPassword}), http.StatusUnauthorized)
	})

	t.Run("success revokes every session, including this one", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		this := e.loginToken(t, testEmail, testPassword)
		other, otherCookie := e.loginCookie(t, testEmail, testPassword)

		// Revocation by time compares whole seconds and keeps tokens issued in
		// the same second as the revocation, so let the clock move on.
		e.Advance(time.Second)
		wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(this.AccessToken)), http.StatusNoContent)

		for name, tok := range map[string]string{"this": this.AccessToken, "other": other.AccessToken} {
			if got := e.meStatus(t, tok); got != http.StatusUnauthorized {
				t.Errorf("%s session: GET /me = %d, want 401", name, got)
			}
		}
		wantStatus(t, e.refreshWithBody(t, *this.RefreshToken), http.StatusUnauthorized)
		wantStatus(t, e.do(t, http.MethodPost, "/auth/refresh", nil, withRefreshCookie(otherCookie)), http.StatusUnauthorized)
	})

	t.Run("success does not touch other users", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		e.verifiedUser(t, "bob@example.com")
		alice := e.loginToken(t, testEmail, testPassword)
		bob := e.loginToken(t, "bob@example.com", testPassword)

		wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(alice.AccessToken)), http.StatusNoContent)

		if got := e.meStatus(t, bob.AccessToken); got != http.StatusOK {
			t.Errorf("bob: GET /me = %d, want 200", got)
		}
	})

	t.Run("403 for a wrong current password, and the session stays valid", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		r := e.do(t, http.MethodPut, "/me/password", body("wrong password", testNewPassword), withBearer(tr.AccessToken))
		wantStatus(t, r, http.StatusForbidden)
		checkErrorMesssage(t, r.errorMessage(t), []string{"wrong current password"})

		// 403, not 401: the token is fine and nothing has changed.
		if got := e.meStatus(t, tr.AccessToken); got != http.StatusOK {
			t.Errorf("GET /me after a wrong attempt = %d, want 200", got)
		}
		wantStatus(t, e.refreshWithBody(t, *tr.RefreshToken), http.StatusOK)
		e.loginToken(t, testEmail, testPassword)
	})

	t.Run("401 without a valid access token", func(t *testing.T) {
		e := setupAuthTest(t)
		u := e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		cases := badAuth(t, e, u.ID)
		cases["refresh token"] = []reqOpt{withBearer(*tr.RefreshToken)}

		for name, opts := range cases {
			t.Run(name, func(t *testing.T) {
				wantUnauthorized(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), opts...))
			})
		}
		e.loginToken(t, testEmail, testPassword) // password unchanged
	})

	t.Run("401 comes before 400", func(t *testing.T) {
		e := setupAuthTest(t)
		wantUnauthorized(t, e.do(t, http.MethodPut, "/me/password", map[string]any{}))
	})

	validationTests := []struct {
		name            string
		body            any
		wantErrContains []string
	}{
		{"missing current_password", map[string]any{"new_password": testNewPassword}, []string{"invalid password"}},
		{"empty current_password", body("", testNewPassword), []string{"invalid password"}},
		{"missing new_password", map[string]any{"current_password": testPassword}, []string{"invalid password"}},
		{"empty new_password", body(testPassword, ""), []string{"invalid password"}},
		{"new_password too short", body(testPassword, "1234567"), []string{"invalid password"}},
		{"new_password too long", body(testPassword, strings.Repeat("a", 129)), []string{"invalid password"}},
		// "ﬁ" (U+FB01) is one rune but becomes "fi" under NFKC: 65 runes -> 130.
		{"new_password too long after NFKC", body(testPassword, strings.Repeat("\ufb01", 65)), []string{"invalid password"}},
		{"current_password too long", body(strings.Repeat("a", 129), testNewPassword), []string{"invalid password"}},
		{"current_password too long after NFKC", body(strings.Repeat("\ufb01", 65), testNewPassword), []string{"invalid password"}},
		{"wrong type", map[string]any{"current_password": 1, "new_password": testNewPassword}, []string{"current_password"}},
		{"malformed JSON", `{"current_password":`, []string{"malformed json"}},
		{"empty body", nil, []string{"body is required"}},
	}
	for _, tt := range validationTests {
		t.Run("400: "+tt.name, func(t *testing.T) {
			e := setupAuthTest(t)
			e.verifiedUser(t, testEmail)
			tr := e.loginToken(t, testEmail, testPassword)

			r := e.do(t, http.MethodPut, "/me/password", tt.body, withBearer(tr.AccessToken))
			wantStatus(t, r, http.StatusBadRequest)
			checkErrorMesssage(t, r.errorMessage(t), tt.wantErrContains)

			if got := e.meStatus(t, tr.AccessToken); got != http.StatusOK {
				t.Errorf("a rejected request must not revoke the session, GET /me = %d", got)
			}
		})
	}

	// A current_password that is merely short is a wrong password: 403, not 400.
	t.Run("403: a short current_password is wrong, not invalid", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		for _, pw := range []string{"a", "1234567"} {
			wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(pw, testNewPassword), withBearer(tr.AccessToken)), http.StatusForbidden)
		}
	})

	t.Run("new_password of exactly 8 and 128 characters is accepted", func(t *testing.T) {
		for _, n := range []int{8, 128} {
			e := setupAuthTest(t)
			e.verifiedUser(t, testEmail)
			tr := e.loginToken(t, testEmail, testPassword)
			wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, strings.Repeat("a", n)), withBearer(tr.AccessToken)), http.StatusNoContent)
		}
	})

	t.Run("the same new password as the current one is accepted", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		// The spec sets no "must differ" rule, so this is valid.
		wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, testPassword), withBearer(tr.AccessToken)), http.StatusNoContent)
	})

	t.Run("wrong attempts are limited like logins: 429 with Retry-After", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		var got apiResp
		for range rateLimitProbes {
			got = e.do(t, http.MethodPut, "/me/password", body("wrong password", testNewPassword), withBearer(tr.AccessToken))
			if got.Status == http.StatusTooManyRequests {
				break
			}
			wantStatus(t, got, http.StatusForbidden)
		}
		wantStatus(t, got, http.StatusTooManyRequests)
		wantRetryAfter(t, got)

		// While waiting, even the right current password is refused.
		r := e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(tr.AccessToken))
		wantStatus(t, r, http.StatusTooManyRequests)
		wantRetryAfter(t, r)
	})

	t.Run("429 comes from the per-email limiter, well before the per-IP limit", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)

		attempts := 0
		for ; attempts < rateLimitProbes; attempts++ {
			r := e.do(t, http.MethodPut, "/me/password", body("wrong password", testNewPassword), withBearer(tr.AccessToken))
			if r.Status == http.StatusTooManyRequests {
				break
			}
		}
		if attempts > 10 {
			t.Errorf("first 429 after %d wrong attempts, want it from the email limiter (a few attempts)", attempts)
		}
	})

	t.Run("wrong attempts share the budget of logins", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		exhaustLoginLimit(t, e, testEmail)

		r := e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(tr.AccessToken))
		wantStatus(t, r, http.StatusTooManyRequests)
		wantRetryAfter(t, r)
	})

	t.Run("a wait expires: the right current password works again", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		last := exhaustLoginLimit(t, e, testEmail)
		wantStatus(t, last, http.StatusTooManyRequests)

		e.Advance(2 * time.Minute)
		wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(tr.AccessToken)), http.StatusNoContent)
	})

	t.Run("a success resets the counter", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		for range 3 { // under the 5 free attempts
			wantStatus(t, e.do(t, http.MethodPut, "/me/password", body("wrong password", testNewPassword), withBearer(tr.AccessToken)), http.StatusForbidden)
		}
		wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(tr.AccessToken)), http.StatusNoContent)

		// Back to a fresh budget: three more wrong logins are still plain 401s.
		for range 3 {
			wantStatus(t, e.do(t, http.MethodPost, "/auth/login", map[string]any{"email": testEmail, "password": "wrong password"}), http.StatusUnauthorized)
		}
	})

	t.Run("a rejected body does not count as an attempt", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		for range 10 {
			wantStatus(t, e.do(t, http.MethodPut, "/me/password", body("", testNewPassword), withBearer(tr.AccessToken)), http.StatusBadRequest)
			wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, "short"), withBearer(tr.AccessToken)), http.StatusBadRequest)
		}
		wantStatus(t, e.do(t, http.MethodPut, "/me/password", body(testPassword, testNewPassword), withBearer(tr.AccessToken)), http.StatusNoContent)
	})

	t.Run("GET and POST are not allowed", func(t *testing.T) {
		e := setupAuthTest(t)
		wantStatus(t, e.do(t, http.MethodGet, "/me/password", nil), http.StatusMethodNotAllowed)
		wantStatus(t, e.do(t, http.MethodPost, "/me/password", nil), http.StatusMethodNotAllowed)
	})
}

// --- the task endpoints are protected by the same bearer token ----------------

func TestTasksRequireAuth(t *testing.T) {
	someID := "01a142e5-6a3e-7218-9722-c0215445fae0"
	endpoints := []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/tasks", nil},
		{http.MethodPost, "/tasks", map[string]any{"title": "t"}},
		{http.MethodGet, "/tasks/" + someID, nil},
		{http.MethodPatch, "/tasks/" + someID, map[string]any{"title": "t"}},
		{http.MethodDelete, "/tasks/" + someID, nil},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path+" without a token", func(t *testing.T) {
			e := setupAuthTest(t)
			wantUnauthorized(t, e.do(t, ep.method, ep.path, ep.body))
		})
		t.Run(ep.method+" "+ep.path+" with a garbage token", func(t *testing.T) {
			e := setupAuthTest(t)
			wantUnauthorized(t, e.do(t, ep.method, ep.path, ep.body, withBearer("garbage")))
		})
	}

	t.Run("a valid token is accepted", func(t *testing.T) {
		e := setupAuthTest(t)
		e.verifiedUser(t, testEmail)
		tr := e.loginToken(t, testEmail, testPassword)
		wantStatus(t, e.do(t, http.MethodGet, "/tasks", nil, withBearer(tr.AccessToken)), http.StatusOK)
	})

	t.Run("401 comes before request validation", func(t *testing.T) {
		e := setupAuthTest(t)
		wantUnauthorized(t, e.do(t, http.MethodGet, "/tasks?limit=abc", nil))
	})

	t.Run("public endpoints need no token", func(t *testing.T) {
		e := setupAuthTest(t)
		for _, p := range []string{"/auth/register", "/auth/login", "/auth/verify-email", "/auth/resend-verification",
			"/auth/refresh", "/auth/logout", "/auth/forgot-password", "/auth/reset-password"} {
			r := e.do(t, http.MethodPost, p, map[string]any{})
			if r.Status == http.StatusUnauthorized && p != "/auth/refresh" {
				t.Errorf("POST %s without a token = 401, want it to be public", p)
			}
		}
	})
}
