package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dung204/taskapi/internal/metrics"
)

func metricsMiddleware(m *metrics.HTTP) middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.InFlightInc()
			defer m.InFlightDec()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			start := time.Now()
			next.ServeHTTP(rec, r)

			m.ObserveRequests(
				metricMethod(r.Method),
				routePattern(r),
				strconv.Itoa(rec.status),
				time.Since(start),
			)
		})
	}
}

func metricMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodHead, http.MethodDelete, http.MethodOptions:
		return method
	default:
		return "OTHER"
	}
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unknown"
}
