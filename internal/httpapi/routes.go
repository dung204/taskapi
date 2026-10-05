package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/dung204/taskapi/internal/task"
)

type handlerFunc = func(w http.ResponseWriter, r *http.Request)

func NewHandler(
	store task.Store,
	logger *slog.Logger,
	pinger storePinger,
) http.Handler {
	mux := http.NewServeMux()

	withRequestID := requestIDMiddleware
	withLogging := loggingMiddleware(logger)
	withRecover := recoverMiddleware(logger)

	withMiddlewares := func(f handlerFunc) http.Handler {
		return withRequestID(withLogging(withRecover(http.HandlerFunc(f))))
	}

	healthHandler := newHealthHandler(pinger)
	taskHandler := newTaskHandler(store)

	mux.Handle("GET /healthz", withMiddlewares(healthHandler.CheckHealth))

	mux.Handle("POST /tasks", withMiddlewares(taskHandler.Create))
	mux.Handle("GET /tasks", withMiddlewares(taskHandler.GetList))
	mux.Handle("GET /tasks/{id}", withMiddlewares(taskHandler.GetOne))
	mux.Handle("PATCH /tasks/{id}", withMiddlewares(taskHandler.Update))
	mux.Handle("DELETE /tasks/{id}", withMiddlewares(taskHandler.Delete))

	return mux
}
