package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil/logtest"
)

func setupTestWithStore(t *testing.T, store task.Store) (http.Handler, *logtest.Recorder) {
	t.Helper()
	rec := logtest.NewRecorder()
	logger := slog.New(NewRequestIDContextHandler(rec))
	return newAuthenticatedHandler(t, store, logger), rec
}

func setupTest(t *testing.T) (h http.Handler, store task.Store, rec *logtest.Recorder) {
	t.Helper()
	rec = logtest.NewRecorder()
	logger := slog.New(NewRequestIDContextHandler(rec))
	store = memory.NewTaskStore()
	h = newAuthenticatedHandler(t, store, logger)
	return h, store, rec
}

// newAuthenticatedHandler builds the handler for the task tests; see
// withTestAuth.
func newAuthenticatedHandler(t *testing.T, store task.Store, logger *slog.Logger) http.Handler {
	t.Helper()
	return withTestAuth(t, HandlerDeps{Tasks: store, Logger: logger})
}

// withTestAuth builds a handler from deps with a real token verifier and an
// empty revocation list. The task endpoints need a bearer token, so requests
// that carry no Authorization header get a valid one; the auth rules
// themselves are tested through setupAuthTest.
// testUserID is the user that the task tests authenticate as; tasks seeded for
// them must be owned by it.
var testUserID = uuid.MustParse("01a142fd-186e-7169-95c2-a186f9103a98")

func withTestAuth(t *testing.T, deps HandlerDeps) http.Handler {
	t.Helper()

	pub, priv := newTestKeys(t)
	jwtService := auth.NewJWTService(auth.JWTServiceOpts{
		PrivateKey: priv, PublicKey: pub,
		Issuer: "taskapi-test", Audiences: []string{"taskapi-test"}, TTL: time.Hour,
	})

	token, _, err := jwtService.Issue(testUserID, uuid.NewV7(), time.Now())
	if err != nil {
		t.Fatalf("issue test token: %v", err)
	}

	deps.Verifier = jwtService
	deps.Revoker = newFakeRevoker()
	h := NewHandler(deps)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		h.ServeHTTP(w, r)
	})
}

func prepareTestRequest(tb testing.TB, method, target string, body any) (*httptest.ResponseRecorder, *http.Request) {
	tb.Helper()

	var r io.Reader
	switch v := body.(type) {
	case nil:

	case string:
		r = strings.NewReader(v)

	default:
		b, err := json.Marshal(v)
		if err != nil {
			tb.Fatalf("marshal body: %v", err)
		}

		r = bytes.NewReader(b)
	}

	req := httptest.NewRequestWithContext(tb.Context(), method, target, r)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	return rec, req
}

func decodeResponseBody[T any](tb testing.TB, body io.ReadCloser) T {
	tb.Helper()

	var dst T
	err := json.NewDecoder(body).Decode(&dst)
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError

	if err != nil {
		switch {
		case errors.Is(err, io.EOF):
			tb.Fatalf("response body is required")
		case errors.Is(err, io.ErrUnexpectedEOF):
			tb.Fatalf("response body contains malformed JSON")
		case errors.As(err, &syntaxErr):
			tb.Fatalf("request body contains malformed JSON (at position %d)", syntaxErr.Offset)
		case errors.As(err, &typeErr):
			if typeErr.Field == "" {
				tb.Fatalf("response body must be a JSON object")
			}
			tb.Fatalf("field %q has the wrong type", typeErr.Field)
		default:
			tb.Fatal("invalid response body")
		}
	}

	return dst
}

func getErrorMessageFromResponseBody(tb testing.TB, body io.ReadCloser) string {
	tb.Helper()
	return decodeResponseBody[oapigen.Error](tb, body).Error
}

func checkErrorMesssage(tb testing.TB, msg string, wantErrContains []string) {
	tb.Helper()
	if len(wantErrContains) == 0 {
		tb.Fatalf("case %q: error case must set wantErrContains", tb.Name())
	}

	gotErrMes := strings.ToLower(msg)
	for _, want := range wantErrContains {
		if !strings.Contains(gotErrMes, strings.ToLower(want)) {
			tb.Errorf("error message: %q, want error message contains %q", gotErrMes, want)
		}
	}
}

func checkErrorMessageHidden(tb testing.TB, msg string, secrets []string) {
	tb.Helper()
	if len(secrets) == 0 {
		tb.Fatalf("case %q: error case must set wantErrContains", tb.Name())
	}

	lower := strings.ToLower(msg)
	for _, s := range secrets {
		if s == "" {
			tb.Fatalf("checkErrorMessageHidden: empty secret would match any body")
		}
		if strings.Contains(lower, strings.ToLower(s)) {
			tb.Errorf("response body leaks %q: %s", s, msg)
		}
	}
}
