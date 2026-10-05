package httpapi

import (
	"context"
	"net/http"
	"time"
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
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		err := handler.pinger.PingContext(ctx)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "store": "unavailable"})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "store": "ok"})
}
