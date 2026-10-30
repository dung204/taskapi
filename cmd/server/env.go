package main

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/url"
	"slices"
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
	opsAddr        string
	rateLimitRPS   float64
	rateLimitBurst int
	corsOrigins    []string
	corsMaxAge     time.Duration
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
	_, port, err := net.SplitHostPort(value)
	if err != nil {
		return fmt.Errorf(`%s must look like ":6060" or "localhost:6060", got %q`, name, value)
	}

	if _, ok := isValidPort(port); !ok {
		return fmt.Errorf(`%s has an invalid port, got %q`, name, value)
	}

	return nil
}

// parseCORSOrigins splits a comma-separated origin list. "*" must be the whole
// value, every other entry must be a bare scheme://host[:port] origin. Origins
// are normalized the way browsers send them (lowercase, no default port) and
// duplicates are dropped.
func parseCORSOrigins(raw string) ([]string, error) {
	if raw == "*" {
		return []string{"*"}, nil
	}

	if strings.Contains(raw, "*") {
		return nil, fmt.Errorf(`CORS_ALLOWED_ORIGINS "*" must be the only value and can not be part of an origin, got %q`, raw)
	}

	var origins []string

	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		u, err := url.Parse(entry)
		if err != nil ||
			(u.Scheme != "http" && u.Scheme != "https") ||
			u.Host == "" || u.Hostname() == "" ||
			u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" ||
			strings.HasSuffix(entry, "?") || strings.HasSuffix(entry, "#") {
			return nil, fmt.Errorf(`CORS_ALLOWED_ORIGINS entries must look like "https://app.example.com" or "http://localhost:5173" (no path or trailing slash), got %q`, entry)
		}

		if port := u.Port(); port != "" {
			if _, ok := isValidPort(port); !ok {
				return nil, fmt.Errorf(`CORS_ALLOWED_ORIGINS has an invalid port, got %q`, entry)
			}
		}

		origin := normalizeOrigin(u)
		if !slices.Contains(origins, origin) {
			origins = append(origins, origin)
		}
	}

	return origins, nil
}

// normalizeOrigin returns u as a browser would send it in the Origin header:
// lowercase scheme and host, and without the port when it is the scheme's default.
func normalizeOrigin(u *url.URL) string {
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)

	if port := u.Port(); (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		host = strings.TrimSuffix(host, ":"+port)
	}

	return scheme + "://" + host
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
	if pprofAddr != "" {
		errs = append(errs, checkListenAddr("PPROF_ADDR", pprofAddr))
	}

	opsAddr := getenv(lookup, "OPS_ADDR", "127.0.0.1:9091")
	errs = append(errs, checkListenAddr("OPS_ADDR", opsAddr))

	rawRateLimitRPS := getenv(lookup, "RATE_LIMIT_RPS", "10")
	rateLimitRPS, err := strconv.ParseFloat(rawRateLimitRPS, 64)
	if err != nil || rateLimitRPS < 0 || math.IsNaN(rateLimitRPS) || math.IsInf(rateLimitRPS, 0) {
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

	corsOrigins, err := parseCORSOrigins(getenv(lookup, "CORS_ALLOWED_ORIGINS", ""))
	if err != nil {
		errs = append(errs, err)
	}

	corsMaxAge := 10 * time.Minute
	if len(corsOrigins) > 0 {
		rawCORSMaxAge := getenv(lookup, "CORS_MAX_AGE", "10m")
		corsMaxAge, err = time.ParseDuration(rawCORSMaxAge)
		if err != nil || corsMaxAge < 0 || corsMaxAge%time.Second != 0 {
			errs = append(errs, fmt.Errorf(`CORS_MAX_AGE must be a non-negative duration in whole seconds like "30s", "10m" or "1h", got %q`, rawCORSMaxAge))
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
		opsAddr:        opsAddr,
		rateLimitRPS:   rateLimitRPS,
		rateLimitBurst: rateLimitBurst,
		corsOrigins:    corsOrigins,
		corsMaxAge:     corsMaxAge,
	}, errors.Join(errs...)
}
