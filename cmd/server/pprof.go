package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"time"
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

func startPprof(ctx context.Context, addr string, logger *slog.Logger) (stop func(), err error) {
	srv := &http.Server{
		Addr:              addr,
		Handler:           pprofHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return nil, fmt.Errorf("open pprof port: %w", err)
	}

	logger.InfoContext(ctx, "pprof started", "addr", ln.Addr().String())

	go func() {
		if serveErr := srv.Serve(ln); !errors.Is(serveErr, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "pprof server failed", "error", err)
		}
	}()

	return func() { _ = srv.Close() }, nil
}
