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
)

func pprofHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

func opsHandler(reg *prometheus.Registry, pinger storePinger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metricsHandler(reg))
	mux.Handle("GET /healthz", healthHandler(pinger))
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
			logger.ErrorContext(ctx, fmt.Sprintf("%s server failed", name), "error", serveErr)
		}
	}()

	return func() { _ = srv.Close() }, nil
}

func startInternalServers(ctx context.Context, cfg config, reg *prometheus.Registry, pinger storePinger, logger *slog.Logger) (stop func(), err error) {
	var stops []func()

	stopAll := func() {
		for _, stop := range slices.Backward(stops) {
			stop()
		}
	}

	if cfg.pprofAddr != "" {
		stopPprof, err := startInternalServer(ctx, "pprof", cfg.pprofAddr, pprofHandler(), logger)
		if err != nil {
			stopAll()
			return nil, err
		}
		stops = append(stops, stopPprof)
	}

	stopOps, err := startInternalServer(ctx, "ops", cfg.opsAddr, opsHandler(reg, pinger), logger)
	if err != nil {
		stopAll()
		return nil, err
	}
	stops = append(stops, stopOps)

	return stopAll, nil
}
