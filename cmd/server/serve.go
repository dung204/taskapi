package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// serve scaffold listeners, background workers and handles shutdown
func serve(ctx context.Context, cfg config, logger *slog.Logger, reg *prometheus.Registry, app app) error {
	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.port),
		Handler:           app.handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("open port: %w", err)
	}
	defer ln.Close()

	logger.Info("server started",
		"port", cfg.port,
		"addr", ln.Addr().String(),
		"store", cfg.tasks.store,
		"log_format", cfg.logging.format,
		"log_level", cfg.logging.level,
		"overdue_scan_worker_enabled", cfg.tasks.overdueScanEnabled,
		"go_version", runtime.Version(),
	)

	errChan := make(chan error, 1)

	go func() {
		if serveErr := srv.Serve(ln); !errors.Is(serveErr, http.ErrServerClosed) {
			errChan <- serveErr
		}
	}()

	//nolint:contextcheck // shutdown does not rely on timeout context
	shutdown := func(srv *http.Server) error {
		logger.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	}

	wg := &sync.WaitGroup{}
	bgCtx, cancelBg := context.WithCancel(ctx)
	defer func() {
		cancelBg()
		wg.Wait()
	}()

	for _, worker := range app.workers {
		wg.Go(func() { worker.Run(bgCtx) })
	}

	stopInternal, err := startInternalServers(ctx, cfg, reg, app.checks, logger)
	if err != nil {
		return fmt.Errorf("internal servers: %w", err)
	}
	defer stopInternal()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received", "cause", context.Cause(ctx))

		err := shutdown(srv)
		if err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		logger.Info("server stopped")
		return nil
	case err = <-errChan:
		shutdownErr := shutdown(srv)
		if shutdownErr != nil {
			logger.Error("shutdown failed", "error", shutdownErr)
		}
		return fmt.Errorf("serve: %w", err)
	}
}
