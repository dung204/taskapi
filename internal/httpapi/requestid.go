package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"uuid"
)

type requestIDKey struct{}

const requestIDHeaderKey = "X-Request-ID"

type requestIDContextHandler struct {
	slog.Handler
}

func NewRequestIDContextHandler(h slog.Handler) slog.Handler {
	return requestIDContextHandler{Handler: h}
}

func (h requestIDContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

func (h requestIDContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return requestIDContextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h requestIDContextHandler) WithGroup(name string) slog.Handler {
	return requestIDContextHandler{Handler: h.Handler.WithGroup(name)}
}

var requestIDRegexp = regexp.MustCompile("^[A-Za-z0-9-]{1,64}$")

var _ middleware = requestIDMiddleware

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeaderKey)

		if !requestIDRegexp.MatchString(id) {
			id = uuid.NewV7().String()
		}

		w.Header().Set(requestIDHeaderKey, id)

		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
