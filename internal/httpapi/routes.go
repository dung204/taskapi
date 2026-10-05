package httpapi

import (
	"net/http"

	"github.com/dung204/taskapi/internal/task"
)

func NewHandler(
	store task.Store,
	pinger storePinger,
) http.Handler {
	mux := http.NewServeMux()
	healthHandler := newHealthHandler(pinger)
	taskHandler := newTaskHandler(store)

	mux.HandleFunc("GET /healthz", healthHandler.CheckHealth)

	mux.HandleFunc("POST /tasks", taskHandler.Create)
	mux.HandleFunc("GET /tasks", taskHandler.GetList)
	mux.HandleFunc("GET /tasks/{id}", taskHandler.GetOne)
	mux.HandleFunc("PATCH /tasks/{id}", taskHandler.Update)
	mux.HandleFunc("DELETE /tasks/{id}", taskHandler.Delete)

	return mux
}
