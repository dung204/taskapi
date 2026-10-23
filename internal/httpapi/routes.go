// Package httpapi is the HTTP layer: routes, middlewares and the task and
// health handlers. Handlers depend only on the [task.Store] interface.
package httpapi

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/dung204/taskapi/internal/httpapi/docs"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/task"
)

type server struct {
	*healthHandler
	*taskHandler
}

var _ oapigen.ServerInterface = (*server)(nil)

func NewHandler(
	store task.Store,
	logger *slog.Logger,
	pinger storePinger,
) http.Handler {
	mux := http.NewServeMux()

	withRequestID := requestIDMiddleware
	withLogging := loggingMiddleware(logger)
	withRecover := recoverMiddleware(logger)

	srv := &server{
		healthHandler: newHealthHandler(pinger),
		taskHandler:   newTaskHandler(store, logger),
	}

	oapigen.HandlerWithOptions(srv, oapigen.StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
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
		},
	})

	mux.HandleFunc("GET /openapi.yaml", docs.Spec)
	mux.Handle("GET /docs/", http.StripPrefix("/docs/", docs.UI()))

	return withRequestID(withLogging(withRecover(mux)))
}
