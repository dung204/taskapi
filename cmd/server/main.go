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

	"golang.org/x/time/rate"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/dung204/taskapi/internal/httpapi"
	"github.com/dung204/taskapi/internal/ratelimit"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/worker"
)

func run() error {
	ctx, stopServer := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopServer()
	go func() {
		<-ctx.Done()
		stopServer()
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
	reg := newRegistry()
	metrics := httpapi.NewMetrics(reg)

	var (
		ml      *ratelimit.MemoryLimiter // have Run() to run as worker & Len() for metrics
		limiter httpapi.Limiter          // for NewHandler()
	)
	if cfg.rateLimitRPS > 0 {
		ml = ratelimit.NewMemoryLimiter(rate.Limit(cfg.rateLimitRPS), cfg.rateLimitBurst)
		limiter = ml

		reg.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name:        "rate_limiter_tracked_clients",
			Help:        "Number of keys currently tracked by the in-memory rate limiter.",
			ConstLabels: prometheus.Labels{"limiter": "global"},
		}, func() float64 { return float64(ml.Len()) }))
	}

	var pinger storePinger

	switch cfg.store {
	case "memory":
		store = memory.NewTaskStore()

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

		reg.MustRegister(postgres.NewPoolCollector(pool))
		store = postgres.NewTaskStore(pool)
		pinger = pool
	}

	handler = httpapi.NewHandler(
		store, logger, metrics, limiter,
		httpapi.CORSMiddlewareOpts{AllowedOrigins: cfg.corsOrigins, MaxAge: cfg.corsMaxAge},
	)
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

	wg := &sync.WaitGroup{}
	bgCtx, cancelBg := context.WithCancel(ctx)
	defer func() {
		cancelBg()
		wg.Wait()
	}()

	if ml != nil {
		wg.Go(func() { ml.Run(bgCtx) })
	}

	if cfg.workerEnabled {
		wg.Go(func() { worker.Run(bgCtx, store, logger, cfg.workerInterval) })
	}

	stopInternal, err := startInternalServers(ctx, cfg, reg, pinger, logger)
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

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(runHealthcheck(os.Getenv))
	}

	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "server failed:", "error:", err)
		os.Exit(1)
	}
}
