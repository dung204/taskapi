package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type HTTP struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func NewHTTP(reg *prometheus.Registry) *HTTP {
	f := promauto.With(reg)
	labels := []string{"method", "route", "status"}

	return &HTTP{
		requests: f.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests, by method, route and status code.",
		}, labels),
		duration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds, by method, route and status code.",
			Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		}, labels),
		inFlight: f.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
	}
}

// The methods below are safe to call on a nil *HTTP and then do nothing, so
// callers can run without metrics.

func (m *HTTP) InFlightInc() {
	if m == nil {
		return
	}
	m.inFlight.Inc()
}

func (m *HTTP) InFlightDec() {
	if m == nil {
		return
	}
	m.inFlight.Dec()
}

func (m *HTTP) ObserveRequests(method, route, status string, d time.Duration) {
	if m == nil {
		return
	}
	m.requests.WithLabelValues(method, route, status).Inc()
	m.duration.WithLabelValues(method, route, status).Observe(d.Seconds())
}
