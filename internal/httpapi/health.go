package httpapi

import (
	"net/http"
)

type healthHandler struct{}

func newHealthHandler() *healthHandler {
	return &healthHandler{}
}

func (*healthHandler) CheckHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
