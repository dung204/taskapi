package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type config struct {
	port           int
	store          string
	dbURL          string
	dbHost         string
	dbName         string
	logFormat      string
	logLevel       slog.Level
	workerInterval time.Duration
}

type lookupFunc = func(string) string

func getenv(lookup lookupFunc, key, fallback string) string {
	v := strings.TrimSpace(lookup(key))
	if v == "" {
		return fallback
	}
	return v
}

func isValidPort(port string) (parsedPort int, ok bool) {
	parsed, err := strconv.Atoi(port)
	if err != nil || parsed < 1 || parsed > 65535 {
		return 0, false
	}
	return parsed, true
}

func parseDBURL(dbURL string) (host, dbName string, err error) {
	errDatabaseURL := errors.New("DATABASE_URL must look like postgres://user:password@host:5432/dbname")

	u, err := url.Parse(dbURL)
	if err != nil {
		return "", "", errDatabaseURL
	}

	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", "", errDatabaseURL
	}

	if u.Hostname() == "" {
		return "", "", errDatabaseURL
	}

	port := u.Port()
	if port != "" {
		if _, ok := isValidPort(port); !ok {
			return "", "", errDatabaseURL
		}
	}

	dbName = strings.TrimPrefix(u.Path, "/")
	if strings.Contains(dbName, "/") {
		return "", "", errDatabaseURL
	}

	return host, dbName, nil
}

func loadConfig(lookup lookupFunc) (config, error) {
	errs := make([]error, 0)

	rawPort := getenv(lookup, "PORT", "8080")
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		errs = append(errs, fmt.Errorf("PORT must be an integer between 1 and 65535, got %q", rawPort))
	}

	store := getenv(lookup, "STORE", "memory")
	var dbURL, dbHost, dbName string

	switch {
	case store != "postgres" && store != "memory":
		errs = append(errs, fmt.Errorf(`MEMORY must be either "memory" or "postgres", got %q`, store))
	case store == "postgres":
		dbURL = getenv(lookup, "DATABASE_URL", "")
		dbHost, dbName, err = parseDBURL(dbURL)
		if err != nil {
			errs = append(errs, err)
		}
	}

	logFormat := getenv(lookup, "LOG_FORMAT", "text")
	if logFormat != "text" && logFormat != "json" {
		errs = append(errs, fmt.Errorf(`LOG_FORMAT must be either "text" or "json", got %q`, logFormat))
	}

	rawLogLevel := getenv(lookup, "LOG_LEVEL", "info")
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(rawLogLevel)); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL is not valid, got %q", logLevel))
	}

	rawWorkerInterval := getenv(lookup, "WORKER_INTERVAL", "30s")
	workerInterval, err := time.ParseDuration(rawWorkerInterval)
	if err != nil || workerInterval <= 0 {
		errs = append(errs, fmt.Errorf(`WORKER_INTERVAL must be a positive duration like "30s", "1m" or "500ms", got %q`, rawWorkerInterval))
	}

	return config{
		port:           port,
		store:          store,
		dbURL:          dbURL,
		dbHost:         dbHost,
		dbName:         dbName,
		logFormat:      logFormat,
		logLevel:       logLevel,
		workerInterval: workerInterval,
	}, errors.Join(errs...)
}
