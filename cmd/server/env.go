package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	workerEnabled  bool
	workerInterval time.Duration
	pprofAddr      string
	metricsAddr    string
	rateLimitRPS   float64
	rateLimitBurst int
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

func checkListenAddr(name, value string) error {
	if value == "" {
		return nil
	}

	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf(`%s must look like ":6060" or "localhost:6060", got %q`, name, value)
	}

	if _, ok := isValidPort(port); !ok {
		return fmt.Errorf(`%s has an invalid port, got %q`, name, value)
	}

	return nil
}

var errDatabaseURL = errors.New("DATABASE_URL must look like postgres://user:password@host:5432/dbname")

func parseDBURL(dbURL string) (host, dbName string, err error) {

	u, err := url.Parse(dbURL)
	if err != nil {
		return "", "", errDatabaseURL
	}

	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", "", errDatabaseURL
	}

	host = u.Hostname()
	if host == "" {
		return "", "", errDatabaseURL
	}

	port := u.Port()
	if port != "" {
		if _, ok := isValidPort(port); !ok {
			return "", "", errDatabaseURL
		}
	}

	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" || strings.Contains(dbName, "/") {
		return "", "", errDatabaseURL
	}

	return host, dbName, nil
}

func loadConfig(lookup lookupFunc) (config, error) {
	errs := make([]error, 0)

	rawPort := getenv(lookup, "PORT", "8080")
	port, ok := isValidPort(rawPort)
	if !ok {
		errs = append(errs, fmt.Errorf("PORT must be an integer between 1 and 65535, got %q", rawPort))
	}

	store := getenv(lookup, "STORE", "memory")
	var dbURL, dbHost, dbName string
	var dbErr error

	switch {
	case store != "postgres" && store != "memory":
		errs = append(errs, fmt.Errorf(`STORE must be either "memory" or "postgres", got %q`, store))
	case store == "postgres":
		dbURL = getenv(lookup, "DATABASE_URL", "")
		dbHost, dbName, dbErr = parseDBURL(dbURL)
		if dbErr != nil {
			errs = append(errs, dbErr)
		}
	}

	logFormat := getenv(lookup, "LOG_FORMAT", "text")
	if logFormat != "text" && logFormat != "json" {
		errs = append(errs, fmt.Errorf(`LOG_FORMAT must be either "text" or "json", got %q`, logFormat))
	}

	rawLogLevel := getenv(lookup, "LOG_LEVEL", "info")
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(rawLogLevel)); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL is not valid, got %q", rawLogLevel))
	}

	rawWorkerEnabled := getenv(lookup, "WORKER_ENABLED", "true")
	workerEnabled, err := strconv.ParseBool(rawWorkerEnabled)
	if err != nil {
		errs = append(errs, fmt.Errorf(`WORKER_ENABLED must be either "true" or "false", got %q`, rawWorkerEnabled))
	}

	workerInterval := 30 * time.Second
	if workerEnabled {
		rawWorkerInterval := getenv(lookup, "WORKER_INTERVAL", "30s")
		workerInterval, err = time.ParseDuration(rawWorkerInterval)
		if err != nil || workerInterval <= 0 {
			errs = append(errs, fmt.Errorf(`WORKER_INTERVAL must be a positive duration like "30s", "1m" or "500ms", got %q`, rawWorkerInterval))
		}
	}

	pprofAddr := getenv(lookup, "PPROF_ADDR", "")
	errs = append(errs, checkListenAddr("PPROF_ADDR", pprofAddr))

	metricsAddr := getenv(lookup, "METRICS_ADDR", "")
	errs = append(errs, checkListenAddr("METRICS_ADDR", metricsAddr))

	rawRateLimitRPS := getenv(lookup, "RATE_LIMIT_RPS", "10")
	rateLimitRPS, err := strconv.ParseFloat(rawRateLimitRPS, 64)
	if err != nil || rateLimitRPS < 0 {
		errs = append(errs, fmt.Errorf(`RATE_LIMIT_RPS must be a positive float number like 0.5, 1 or set to 0 to disable rate limiting, got %q`, rawRateLimitRPS))
	}

	var rateLimitBurst int
	if rateLimitRPS > 0 {
		rawRateLimitBurst := getenv(lookup, "RATE_LIMIT_BURST", "20")
		rateLimitBurst, err = strconv.Atoi(rawRateLimitBurst)
		if err != nil || rateLimitBurst < 1 {
			errs = append(errs, fmt.Errorf(`RATE_LIMIT_BURST must be a positive integer, got %q`, rawRateLimitBurst))
		}
	}

	return config{
		port:           port,
		store:          store,
		dbURL:          dbURL,
		dbHost:         dbHost,
		dbName:         dbName,
		logFormat:      logFormat,
		logLevel:       logLevel,
		workerEnabled:  workerEnabled,
		workerInterval: workerInterval,
		pprofAddr:      pprofAddr,
		metricsAddr:    metricsAddr,
		rateLimitRPS:   rateLimitRPS,
		rateLimitBurst: rateLimitBurst,
	}, errors.Join(errs...)
}
