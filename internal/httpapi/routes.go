// Package httpapi is the HTTP layer: routes, middlewares and the task and
// health handlers. Handlers depend only on the [task.Store] interface.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/dung204/taskapi/internal/httpapi/docs"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/task"
)

type server struct {
	*healthHandler
	*taskHandler
}

var _ oapigen.StrictServerInterface = (*server)(nil)

func NewHandler(
	store task.Store,
	logger *slog.Logger,
	pinger storePinger,
) http.Handler {
	srv := &server{
		healthHandler: newHealthHandler(pinger),
		taskHandler:   newTaskHandler(store, logger),
	}

	r := chi.NewRouter()

	r.Use(requestIDMiddleware, loggingMiddleware(logger), recoverMiddleware(logger))

	strict := oapigen.NewStrictHandlerWithOptions(srv, nil, oapigen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  srv.handleRequestError,
		ResponseErrorHandlerFunc: srv.handleResponseError(logger),
	})
	oapigen.HandlerWithOptions(strict, oapigen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: srv.handleParamError,
	})

	r.Get("/openapi.yaml", docs.Spec)
	r.Method(http.MethodGet, "/docs", http.RedirectHandler("/docs/", http.StatusMovedPermanently))
	r.Method(http.MethodGet, "/docs/*", http.StripPrefix("/docs/", docs.UI()))

	return r
}

func (s *server) handleParamError(w http.ResponseWriter, r *http.Request, err error) {
	var errInvalid *oapigen.InvalidParamFormatError
	var errRequired *oapigen.RequiredParamError

	switch {
	case errors.As(err, &errInvalid):
		writeError(w, http.StatusBadRequest, "invalid "+errInvalid.ParamName)
	case errors.As(err, &errRequired):
		writeError(w, http.StatusBadRequest, errRequired.ParamName+" is required")
	default:
		writeError(w, http.StatusBadRequest, "invalid request")
	}
}

func (s *server) handleRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	var (
		msg       string
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)

	switch {
	case errors.Is(err, io.EOF):
		msg = "request body is required"
	case errors.Is(err, io.ErrUnexpectedEOF):
		msg = "request body contains malformed JSON"
	case errors.As(err, &syntaxErr):
		msg = fmt.Sprintf("request body contains malformed JSON (at position %d)", syntaxErr.Offset)
	case errors.As(err, &typeErr):
		msg = fmt.Sprintf(`field "%s" has the wrong type`, typeErr.Field)
		if typeErr.Field == "" {
			msg = "request body must be a JSON object"
		}
	default:
		msg = "invalid request body"
	}

	writeError(w, http.StatusBadRequest, msg)
}

func (s *server) handleResponseError(l *slog.Logger) func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		ctx := r.Context()

		switch {
		case errors.Is(err, task.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())

		case errors.Is(err, task.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())

		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusServiceUnavailable, "service unavailable")

		case errors.Is(err, task.ErrServiceUnavailable):
			writeError(w, http.StatusServiceUnavailable, "service unavailable")
			l.ErrorContext(ctx, "store error", "error", err)

		case errors.Is(err, context.Canceled):

		case err != nil:
			writeError(w, http.StatusInternalServerError, "internal server error")
			l.ErrorContext(ctx, "store error", "error", err)
		}
	}
}
