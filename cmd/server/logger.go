package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/dung204/taskapi/internal/httpapi"
)

func newLogger(cfg config) *slog.Logger {
	switch cfg.logFormat {
	case "text":
		return slog.New(httpapi.NewRequestIDContextHandler(slog.NewTextHandler(os.Stderr, nil)))
	case "json":
		return slog.New(httpapi.NewRequestIDContextHandler(slog.NewJSONHandler(os.Stderr, nil)))
	default:
		fmt.Fprintf(os.Stderr, `undefined LOG_FORMAT: '%s'\n`, cfg.logFormat)
		os.Exit(1)
	}

	return nil
}
