package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/dung204/taskapi/internal/httpapi"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/task"
)

func main() {
	cfg := loadConfig()

	logger, err := newLogger(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var store task.Store
	var handler http.Handler

	switch cfg.store {
	case "memory":
		store = memory.NewTaskStore()
		handler = httpapi.NewHandler(store, logger, nil)

	case "postgres":
		start := time.Now()
		db, host, dbName, err := connectDB(cfg.databaseURL)
		elapsed := time.Since(start)

		if err != nil {
			logger.Error("connect to database failed", "error", err)
			os.Exit(1)
		}
		defer db.Close()

		logger.Info("connect to database successfully",
			"max_open_conns", db.Stats().MaxOpenConnections,
			"connect_duration_ms", float64(elapsed)/float64(time.Millisecond),
			"host", host,
			"database", dbName,
		)

		store = postgres.NewTaskStore(db)
		handler = httpapi.NewHandler(store, logger, db)

	default:
		logger.Error(fmt.Sprintf("undefined STORE '%q'", cfg.store), "error", err)
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

	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		logger.Error("port listened failed", "error", err)
	}
	defer ln.Close()

	logger.Info("server started",
		"store", cfg.store,
		"log_format", cfg.logFormat,
		"log_level", cfg.logLevel,
		"go_version", runtime.Version(),
	)

	err = server.Serve(ln)
	if !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped with error", "error", err)
		os.Exit(1)
	}

}
