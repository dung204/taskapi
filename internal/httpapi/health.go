package httpapi

import (
	"context"
	"net/http"
)

type storePinger interface {
	PingContext(ctx context.Context) error
}

type healthHandler struct {
	pinger storePinger
}

func newHealthHandler(pinger storePinger) *healthHandler {
	return &healthHandler{
		pinger,
	}
}

func (handler *healthHandler) CheckHealth(w http.ResponseWriter, r *http.Request) {
	if handler.pinger != nil {
		err := handler.pinger.PingContext(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "store": "unavailable"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "store": "ok"})
}
