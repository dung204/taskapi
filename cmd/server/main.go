// Command server runs the task-management REST API.
//
// It reads its configuration from environment variables, picks the in-memory or
// Postgres store, serves HTTP, and shuts down gracefully on SIGINT/SIGTERM.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
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

	reg := newRegistry()

	infra, closeInfra, err := openInfra(ctx, cfg, logger, reg)
	if err != nil {
		return err
	}
	defer closeInfra()

	app, err := buildApp(cfg, logger, reg, infra)
	if err != nil {
		return err
	}

	return serve(ctx, cfg, logger, reg, app)
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
