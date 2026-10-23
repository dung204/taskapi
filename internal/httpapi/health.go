package httpapi

import (
	"context"
	"net/http"
	"time"
)

type storePinger interface {
	Ping(ctx context.Context) error
}

type healthHandler struct {
	pinger storePinger
}

func newHealthHandler(p storePinger) *healthHandler {
	return &healthHandler{
		pinger: p,
	}
}

func (handler *healthHandler) CheckHealth(w http.ResponseWriter, r *http.Request) {
	if handler.pinger != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := handler.pinger.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "store": "unavailable"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "store": "ok"})
}
