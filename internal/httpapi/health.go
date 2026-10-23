package httpapi

import (
	"context"
	"time"

	"github.com/dung204/taskapi/internal/httpapi/oapigen"
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

func (handler *healthHandler) CheckHealth(ctx context.Context, request oapigen.CheckHealthRequestObject) (oapigen.CheckHealthResponseObject, error) {
	if handler.pinger != nil {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := handler.pinger.Ping(ctx); err != nil {
			return oapigen.CheckHealth503JSONResponse{
				Status: oapigen.HealthStatusUnavailable,
				Store:  oapigen.HealthStoreUnavailable,
			}, nil
		}
	}

	return oapigen.CheckHealth200JSONResponse{
		Status: oapigen.HealthStatusOk,
		Store:  oapigen.HealthStoreOk,
	}, nil
}
