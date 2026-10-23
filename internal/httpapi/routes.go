// Package httpapi is the HTTP layer: routes, middlewares and the task and
// health handlers. Handlers depend only on the [task.Store] interface.
package httpapi

import (
	"errors"
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

var _ oapigen.ServerInterface = (*server)(nil)

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

	oapigen.HandlerWithOptions(srv, oapigen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: handleParamError,
	})

	r.Get("/openapi.yaml", docs.Spec)
	r.Method(http.MethodGet, "/docs", http.RedirectHandler("/docs/", http.StatusMovedPermanently))
	r.Method(http.MethodGet, "/docs/", docs.UI())

	return r
}

func handleParamError(w http.ResponseWriter, r *http.Request, err error) {
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
