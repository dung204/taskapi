package httpapi

import (
	"net/http"
)

func NewHandler(
	// TODO: deps (store, logger, etc.) here 
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", checkHealth)

	return mux;
}