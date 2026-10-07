package httpapi

import (
	"log/slog"
	"net/http"

	"github.com/dung204/taskapi/internal/task"
)

func NewHandler(
	store task.Store,
	logger *slog.Logger,
	pinger storePinger,
) http.Handler {
	mux := http.NewServeMux()

	withRequestID := requestIDMiddleware
	withLogging := loggingMiddleware(logger)
	withRecover := recoverMiddleware(logger)

	healthHandler := newHealthHandler(pinger)
	taskHandler := newTaskHandler(store, logger)

	mux.HandleFunc("GET /healthz", healthHandler.CheckHealth)

	mux.HandleFunc("POST /tasks", taskHandler.Create)
	mux.HandleFunc("GET /tasks", taskHandler.GetList)
	mux.HandleFunc("GET /tasks/{id}", taskHandler.GetOne)
	mux.HandleFunc("PATCH /tasks/{id}", taskHandler.Update)
	mux.HandleFunc("DELETE /tasks/{id}", taskHandler.Delete)

	return withRequestID(withLogging(withRecover(mux)))
}
