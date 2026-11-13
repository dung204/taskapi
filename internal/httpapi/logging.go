package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// statusRecorder remembers the status code of a response. The metrics
// middleware uses it too.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *statusRecorder) Unwrap() http.ResponseWriter {
	return rec.ResponseWriter
}

func loggingMiddleware(l *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			start := time.Now()
			next.ServeHTTP(rec, r)
			elapsed := time.Since(start)

			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}

			l.Log(r.Context(), level, "request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", float64(elapsed)/float64(time.Millisecond),
			)
		})
	}
}
