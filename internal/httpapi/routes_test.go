package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"uuid"

	"github.com/dung204/taskapi/internal/logtest"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/task"
)

type panicStore struct {
	task.Store
}

func (s panicStore) Get(context.Context, uuid.UUID) (task.Task, error) {
	panic("boom")
}

func TestHandler(t *testing.T) {

	t.Run("non-exist path", func(t *testing.T) {
		h, _, _ := setupTest(t)
		rec, req := prepareTestRequest(t, http.MethodGet, "/abc", nil)
		h.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusNotFound)
		}
	})

	t.Run("wrong method", func(t *testing.T) {
		h, _, _ := setupTest(t)
		rec, req := prepareTestRequest(t, http.MethodPost, "/healthz", nil)
		h.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Fatalf("status code = %d, want %d", res.StatusCode, http.StatusMethodNotAllowed)
		}
	})

	t.Run("X-Request-ID header returned and logged", func(t *testing.T) {
		h, _, logRec := setupTest(t)
		reqIDHeader := "X-Request-ID"
		wantReqID := "abc"
		rec, req := prepareTestRequest(t, http.MethodPost, "/healthz", nil)
		req.Header.Set(reqIDHeader, wantReqID)
		h.ServeHTTP(rec, req)

		res := rec.Result()
		defer res.Body.Close()

		got := res.Header.Get(reqIDHeader)
		if got == "" {
			t.Fatalf("%s header is missing", reqIDHeader)
		}

		if got != wantReqID {
			t.Fatalf("%s = %q, want %q", reqIDHeader, got, wantReqID)
		}

		logtest.CheckEntries(t, logRec.Entries(), []logtest.Entry{{
			Level: slog.LevelInfo,
			Msg:   "request completed",
			Attrs: map[string]any{"request_id": wantReqID},
		}}, logtest.OnlyAttrs("request_id"))
	})

	t.Run("panic recovered", func(t *testing.T) {
		store := panicStore{Store: memory.NewTaskStore()}
		h, logRec := setupTestWithStore(t, store)
		rec, req := prepareTestRequest(t, http.MethodGet, "/tasks/018f0000-0000-7000-8000-000000000000", nil)
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}

		res := rec.Result()
		defer res.Body.Close()

		errorMsg := getErrorMessageFromResponseBody(t, res.Body)
		if !strings.Contains(errorMsg, "internal server error") {
			t.Fatalf(`body = %q, want generic message "internal server error"`, errorMsg)
		}

		if strings.Contains(errorMsg, "boom") {
			t.Fatalf("body leaks panic value: %q", errorMsg)
		}

		logtest.CheckEntries(
			t,
			logRec.Entries(),
			[]logtest.Entry{
				{
					Level: slog.LevelError,
					Msg:   "panic recovered",
					Attrs: map[string]any{"panic": "boom"},
				},
				{
					Level: slog.LevelError,
					Msg:   "request completed",
					Attrs: map[string]any{"status": int64(500)},
				},
			},
			logtest.IgnoreAttrs("duration_ms", "stack", "method", "path", "request_id"),
		)
	})
}
