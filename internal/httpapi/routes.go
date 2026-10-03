package httpapi

import (
	"net/http"
)

func NewHandler(
// TODO: deps (store, logger, etc.) here
) http.Handler {
	mux := http.NewServeMux()
	healthHandler := newHealthHandler()
	taskHandler := newTaskHandler()

	mux.HandleFunc("GET /healthz", healthHandler.CheckHealth)

	mux.HandleFunc("POST /tasks", taskHandler.Create)
	mux.HandleFunc("GET /tasks", taskHandler.GetList)
	// mux.HandleFunc("GET /tasks/{id}", taskHandler.GetOne)
	// mux.HandleFunc("PATCH /tasks/{id}", taskHandler.Update)
	// mux.HandleFunc("DELETE /tasks/{id}", taskHandler.Delete)

	return mux
}
