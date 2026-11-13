package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// middleware is the shape shared by every middleware in this package. They live
// one per file: requestid.go, logging.go, cors.go, metrics_middleware.go,
// auth_middleware.go and limiter.go; the small ones are kept here.
type middleware = func(next http.Handler) http.Handler

func recoverMiddleware(l *slog.Logger) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func(ctx context.Context) {
				if rec := recover(); rec != nil {
					l.ErrorContext(ctx, "panic recovered",
						"panic", rec,
						"stack", string(debug.Stack()),
					)
					writeError(w, http.StatusInternalServerError, "internal server error")
				}
			}(r.Context())
			next.ServeHTTP(w, r)
		})
	}
}

type userAgentKey struct{}

var _ middleware = userAgentMiddleware

func userAgentMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), userAgentKey{}, r.UserAgent())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func userAgentFrom(ctx context.Context) string {
	info, _ := ctx.Value(userAgentKey{}).(string)
	return info
}
