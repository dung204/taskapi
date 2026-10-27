package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"slices"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func pprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

func newRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

func metricsHandler(reg *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	return mux
}

func startInternalServer(ctx context.Context, name, addr string, handler http.Handler, logger *slog.Logger) (stop func(), err error) {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("open %s port: %w", name, err)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	//nolint:sloglint // server name is dynamic
	logger.InfoContext(ctx, fmt.Sprintf("%s started", name), "addr", ln.Addr().String())

	go func() {
		if serveErr := srv.Serve(ln); !errors.Is(serveErr, http.ErrServerClosed) {
			//nolint:sloglint // server name is dynamic
			logger.ErrorContext(ctx, fmt.Sprintf("%s server failed", name), "error", err)
		}
	}()

	return func() { _ = srv.Close() }, nil
}

func startDiagnostics(ctx context.Context, cfg config, reg *prometheus.Registry, logger *slog.Logger) (stop func(), err error) {
	var stops []func()

	stopAll := func() {
		for _, stop := range slices.Backward(stops) {
			stop()
		}
	}

	if cfg.pprofAddr != "" {
		stop, err := startInternalServer(ctx, "pprof", cfg.pprofAddr, pprofHandler(), logger)
		if err != nil {
			stopAll()
			return nil, err
		}
		stops = append(stops, stop)
	}

	if cfg.metricsAddr != "" {
		stop, err := startInternalServer(ctx, "metrics", cfg.metricsAddr, metricsHandler(reg), logger)
		if err != nil {
			stopAll()
			return nil, err
		}
		stops = append(stops, stop)
	}

	return stopAll, nil
}
