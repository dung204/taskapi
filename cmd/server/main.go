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
	"syscall"
	"time"

	"github.com/dung204/taskapi/internal/httpapi"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/task"
)

func run(ctx context.Context, cfg config, l *slog.Logger) error {
	var store task.Store
	var handler http.Handler

	switch cfg.store {
	case "memory":
		store = memory.NewTaskStore()
		handler = httpapi.NewHandler(store, l, nil)

	case "postgres":
		if cfg.databaseURL == "" {
			return errors.New("DATABASE_URL is required when STORE=postgres")
		}

		host, dbName, err := parseDBURL(cfg.databaseURL)
		if err != nil {
			return err
		}

		l.Debug("connecting to database",
			"host", host,
			"database", dbName,
		)

		start := time.Now()
		db, err := connectDB(cfg.databaseURL)
		elapsed := time.Since(start)

		if err != nil {
			return fmt.Errorf("connect to database %q on %q: %w", dbName, host, err)
		}
		defer func() {
			err := db.Close()
			if err != nil {
				l.Warn("database close failed", "error", err)
			}

			l.Error("database closed")
		}()

		l.Info("database connected",
			"max_open_conns", db.Stats().MaxOpenConnections,
			"connect_duration_ms", float64(elapsed)/float64(time.Millisecond),
			"host", host,
			"database", dbName,
		)

		store = postgres.NewTaskStore(db)
		handler = httpapi.NewHandler(store, l, db)

	default:
		return fmt.Errorf("invalid STORE %q", cfg.store)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.port,
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

	l.Info("server started",
		"port", cfg.port,
		"addr", ln.Addr().String(),
		"store", cfg.store,
		"log_format", cfg.logFormat,
		"log_level", cfg.logLevel,
		"go_version", runtime.Version(),
	)

	errChan := make(chan error, 1)

	go func() {
		if serveErr := srv.Serve(ln); !errors.Is(serveErr, http.ErrServerClosed) {
			errChan <- serveErr
		}
	}()

	shutdown := func(srv *http.Server) error {
		l.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		return srv.Shutdown(shutdownCtx)
	}

	select {
	case <-ctx.Done():
		l.Info("shutdown signal received", "cause", context.Cause(ctx))

		err := shutdown(srv)
		if err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		l.Info("server stopped")
		return nil
	case err = <-errChan:
		shutdownErr := shutdown(srv)
		if shutdownErr != nil {
			l.Error("shutdown failed", "error", shutdownErr)
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

	cfg := loadConfig()

	l, err := newLogger(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	err = run(ctx, cfg, l)
	if err != nil {
		l.Error("server failed", "error", err)
		os.Exit(1)
	}
}
