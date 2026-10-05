package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/dung204/taskapi/internal/httpapi"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/task"
)

func main() {
	cfg := loadConfig()

	var store task.Store
	var handler http.Handler
	var logger *slog.Logger

	switch cfg.logFormat {
	case "text":
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	case "json":
		logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	default:
		fmt.Fprintf(os.Stderr, `undefined LOG_FORMAT: '%s'\n`, cfg.logFormat)
		os.Exit(1)
	}

	switch cfg.store {
	case "memory":
		store = memory.NewTaskStore()
		handler = httpapi.NewHandler(store, logger, nil)

	case "postgres":
		if cfg.databaseURL == "" {
			fmt.Fprintln(os.Stderr, "DATABASE_URL is empty.")
			os.Exit(1)
		}

		db, err := connectDB(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer db.Close()

		store = postgres.NewTaskStore(db)
		handler = httpapi.NewHandler(store, logger, db)

	default:
		fmt.Fprintf(os.Stderr, `undefined STORE: '%s'\n`, cfg.store)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	err := server.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
