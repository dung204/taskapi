package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/dung204/taskapi/internal/httpapi"
)

func newLogger(cfg config) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.logLevel)); err != nil {
		return nil, fmt.Errorf("invalid LOG_LEVEL: '%q': %w", cfg.logLevel, err)
	}

	opts := &slog.HandlerOptions{Level: level}

	switch cfg.logFormat {
	case "text":
		return slog.New(httpapi.NewRequestIDContextHandler(slog.NewTextHandler(os.Stderr, opts))), nil
	case "json":
		return slog.New(httpapi.NewRequestIDContextHandler(slog.NewJSONHandler(os.Stderr, opts))), nil
	default:
		return nil, fmt.Errorf(`invalid LOG_FORMAT: '%q'`, cfg.logFormat)
	}
}
