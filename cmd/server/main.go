// Command server runs the task-management REST API.
//
// It reads its configuration from environment variables, picks the in-memory or
// Postgres store, serves HTTP, and shuts down gracefully on SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/dung204/taskapi/internal/httpapi"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/worker"
)

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg)
	if err != nil {
		return err
	}

	var store task.Store
	var handler http.Handler

	switch cfg.store {
	case "memory":
		store = memory.NewTaskStore()
		handler = httpapi.NewHandler(store, logger, nil)

	case "postgres":
		logger.Debug("connecting to database",
			"host", cfg.dbHost,
			"database", cfg.dbName,
		)

		start := time.Now()
		pool, err := connectDB(ctx, cfg.dbURL)
		elapsed := time.Since(start)

		if err != nil {
			return fmt.Errorf("connect to database %q on %q: %w", cfg.dbName, cfg.dbHost, err)
		}
		defer func() {
			pool.Close()
			logger.Info("database closed")
		}()

		logger.Info("database connected",
			"max_open_conns", pool.Stat().MaxConns(),
			"connect_duration_ms", float64(elapsed)/float64(time.Millisecond),
			"host", cfg.dbHost,
			"database", cfg.dbName,
		)

		store = postgres.NewTaskStore(pool)
		handler = httpapi.NewHandler(store, logger, pool)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.port),
		Handler:           handler,
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
		"store", cfg.store,
		"log_format", cfg.logFormat,
		"log_level", cfg.logLevel,
		"worker_enabled", cfg.workerEnabled,
		"go_version", runtime.Version(),
	)

	errChan := make(chan error, 1)

	go func() {
		if serveErr := srv.Serve(ln); !errors.Is(serveErr, http.ErrServerClosed) {
			errChan <- serveErr
		}
	}()

	shutdown := func(srv *http.Server) error {
		logger.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	}

	if cfg.workerEnabled {
		workerCtx, cancelWorker := context.WithCancel(ctx)
		wg := &sync.WaitGroup{}
		wg.Go(func() { worker.Run(workerCtx, store, logger, cfg.workerInterval) })
		defer func() {
			cancelWorker()
			wg.Wait()
		}()
	}

	if cfg.pprofAddr != "" {
		stopPprof, err := startPprof(ctx, cfg.pprofAddr, logger)
		if err != nil {
			return err
		}
		defer stopPprof()
	}

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

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "server failed:", "error:", err)
		os.Exit(1)
	}
}
