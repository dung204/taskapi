package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dung204/taskapi/internal/httpapi"
	"github.com/dung204/taskapi/internal/store/memory"
	"github.com/dung204/taskapi/internal/store/postgres"
	"github.com/dung204/taskapi/internal/task"
)

func main() {
	config := loadConfig()

	var store task.Store
	var handler http.Handler

	switch config.store {
	case "memory":
		store = memory.NewTaskStore()
		handler = httpapi.NewHandler(store, nil)

	case "postgres":
		if config.databaseURL == "" {
			fmt.Fprintln(os.Stderr, "DATABASE_URL is empty.")
			os.Exit(1)
		}

		db, err := connectDB(config)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer db.Close()

		store = postgres.NewTaskStore(db)
		handler = httpapi.NewHandler(store, db)

	default:
		fmt.Fprintf(os.Stderr, `undefined STORE: '%s'\n`, config.store)
		os.Exit(1)
	}

	server := &http.Server{
		Addr:              ":" + config.port,
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
