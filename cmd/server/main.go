package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

func run(ctx context.Context, cfg config, logger *slog.Logger) error {
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
		db, err := connectDB(cfg.dbURL)
		elapsed := time.Since(start)

		if err != nil {
			return fmt.Errorf("connect to database %q on %q: %w", cfg.dbName, cfg.dbHost, err)
		}
		defer func() {
			if err := db.Close(); err != nil {
				logger.Warn("database close failed", "error", err)
				return
			}

			logger.Info("database closed")
		}()

		logger.Info("database connected",
			"max_open_conns", db.Stats().MaxOpenConnections,
			"connect_duration_ms", float64(elapsed)/float64(time.Millisecond),
			"host", cfg.dbHost,
			"database", cfg.dbName,
		)

		store = postgres.NewTaskStore(db)
		handler = httpapi.NewHandler(store, logger, db)
	}

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.port),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", srv.Addr)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop()
	}()

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	l, err := newLogger(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := run(ctx, cfg, l); err != nil {
		l.Error("server failed", "error", err)
		os.Exit(1)
	}
}
