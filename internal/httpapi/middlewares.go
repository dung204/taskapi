package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"
	"uuid"
)

type middleware = func(next http.Handler) http.Handler
type ctxKey string

const requestIDKey ctxKey = "request_id"
const requestIDHeaderKey = "X-Request-ID"

var requestIDRegexp = regexp.MustCompile("^[A-Za-z0-9-]{1,64}$")

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeaderKey)

		if !requestIDRegexp.MatchString(id) {
			id = uuid.NewV7().String()
		}

		w.Header().Set(requestIDHeaderKey, id)

		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Write(b []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return rec.ResponseWriter.Write(b)
}

func loggingMiddleware(l *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			id, _ := r.Context().Value(requestIDKey).(string)

			start := time.Now()
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start)

			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}

			l.Log(r.Context(), level, "request completed",
				"request_id", id,
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", float64(elapsed)/float64(time.Millisecond),
			)
		})
	}
}

func recoverMiddleware(l *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					id, _ := r.Context().Value(requestIDKey).(string)
					l.ErrorContext(r.Context(), "panic recovered",
						"request_id", id,
						"panic", rec,
						"stack", string(debug.Stack()),
					)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
