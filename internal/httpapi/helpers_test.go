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

	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/logtest"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
)

func setupTestWithPinger(t *testing.T, pinger storePinger) (h http.Handler, store task.Store, rec *logtest.Recorder) {
	t.Helper()
	rec = logtest.NewRecorder()
	store = memory.NewTaskStore()
	logger := slog.New(NewRequestIDContextHandler(rec))
	h = NewHandler(store, logger, pinger, nil, nil)
	return h, store, rec
}

func setupTestWithStore(t *testing.T, store task.Store) (http.Handler, *logtest.Recorder) {
	t.Helper()
	rec := logtest.NewRecorder()
	logger := slog.New(NewRequestIDContextHandler(rec))
	return NewHandler(store, logger, nil, nil, nil), rec
}

func setupTest(t *testing.T) (h http.Handler, store task.Store, rec *logtest.Recorder) {
	t.Helper()
	rec = logtest.NewRecorder()
	logger := slog.New(NewRequestIDContextHandler(rec))
	store = memory.NewTaskStore()
	h = NewHandler(store, logger, nil, nil, nil)
	return h, store, rec
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

func checkErrorMesssage(t *testing.T, msg string, wantErrContains []string) {
	t.Helper()
	if len(wantErrContains) == 0 {
		t.Fatalf("case %q: error case must set wantErrContains", t.Name())
	}

	gotErrMes := strings.ToLower(msg)
	for _, want := range wantErrContains {
		if !strings.Contains(gotErrMes, strings.ToLower(want)) {
			t.Errorf("error message: %q, want error message contains %q", gotErrMes, want)
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
