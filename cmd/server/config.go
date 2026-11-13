package main

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/mail"
	"net/url"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dung204/taskapi/internal/email"
)

type db struct{ url, host, name string }
type redisCfg struct{ url, host, dbName string }
type logging struct {
	format string
	level  slog.Level
}
type tasks struct {
	store string
	// the background worker that marks overdue tasks
	overdueScanEnabled  bool
	overdueScanInterval time.Duration
}
type rateLimit struct {
	rps   float64
	burst int
}
type cors struct {
	origins []string
	maxAge  time.Duration
}
type addrs struct{ pprof, ops string }
type jwtCfg struct {
	privateKeyPath, publicKeyPath, issuer string
	audiences                             []string
}
type tokens struct {
	accessTTL, refreshTTL time.Duration
	// how long the previous refresh token stays valid after a rotation
	refreshGrace time.Duration
	// lifetimes of the one-time tokens sent by email
	verifyEmailTTL, resetPasswordTTL time.Duration
}
type argon2 struct {
	time, memoryKiB uint32
	threads         uint8
	concurrency     int
}
type loginCfg struct {
	freeAttempts int
	backoff      []time.Duration
	window       time.Duration
}
type mailCfg struct {
	requestCooldown  time.Duration
	requestHourlyMax int
}

type outboxCfg struct {
	// lease is how long a claimed batch belongs to one worker before another may take it
	lease     time.Duration
	batchSize int
	// sendTimeout is the time limit for sending one email of a batch
	sendTimeout time.Duration
	// pollInterval is the fallback ticker of the delivery worker, which looks for
	// due items even when no notification arrived
	pollInterval time.Duration
	// retry policy of a failed send: after maxAttempts the item is marked failed,
	// before that it is rescheduled with a backoff that starts at retryBase and
	// never exceeds retryCap
	maxAttempts int
	retryBase   time.Duration
	retryCap    time.Duration
	// sentRetention is how long a sent item is kept before it is deleted
	sentRetention time.Duration
}

type cleanupCfg struct {
	// interval is how often the cleanup job deletes data that is past its retention
	interval time.Duration
}

type config struct {
	port      int
	tasks     tasks
	db        db
	redis     redisCfg
	logging   logging
	rateLimit rateLimit
	cors      cors
	addrs     addrs
	jwt       jwtCfg
	tokens    tokens
	argon2    argon2
	login     loginCfg
	mail      mailCfg
	smtp      email.SMTPConfig
	outbox    outboxCfg
	cleanup   cleanupCfg
	// appBaseURL is the origin the links in emails point at, without a trailing slash.
	appBaseURL string
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

var errRedisURL = errors.New("REDIS_URL must look like redis://user:password@host:6379/db")

func parseRedisURL(redisURL string) (host, dbName string, err error) {

	u, err := url.Parse(redisURL)
	if err != nil {
		return "", "", errRedisURL
	}

	if u.Scheme != "redis" {
		return "", "", errRedisURL
	}

	host = u.Hostname()
	port := u.Port()
	if port != "" {
		if _, ok := isValidPort(port); !ok {
			return "", "", errRedisURL
		}
	}

	dbName = strings.TrimPrefix(u.Path, "/")
	if dbName == "" || strings.Contains(dbName, "/") {
		return "", "", errRedisURL
	}

	return host, dbName, nil
}

// getUint reads an unsigned integer in [min, max], using fallback when the
// variable is unset. It returns 0 and an error that names the variable when the
// value is not valid.
func getUint(lookup lookupFunc, key, fallback string, min, max uint64) (uint64, error) {
	raw := getenv(lookup, key, fallback)
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || v < min || v > max {
		return 0, fmt.Errorf("%s must be an integer between %d and %d, got %q", key, min, max, raw)
	}
	return v, nil
}

// getPositiveDuration reads a Go duration greater than zero, using fallback
// when the variable is unset.
func getPositiveDuration(lookup lookupFunc, key, fallback string) (time.Duration, error) {
	raw := getenv(lookup, key, fallback)
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf(`%s must be a positive duration like "10m" or "720h", got %q`, key, raw)
	}
	return d, nil
}

// parseAudiences splits a comma-separated audience list. Like the CORS origins,
// spaces and empty entries are ignored and duplicates are dropped.
func parseAudiences(raw string) []string {
	var audiences []string

	for entry := range strings.SplitSeq(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry != "" && !slices.Contains(audiences, entry) {
			audiences = append(audiences, entry)
		}
	}

	return audiences
}

func loadDB(lookup lookupFunc) (db, error) {
	c := db{url: getenv(lookup, "DATABASE_URL", "")}

	var err error
	c.host, c.name, err = parseDBURL(c.url)

	return c, err
}

func loadRedis(lookup lookupFunc) (redisCfg, error) {
	c := redisCfg{url: getenv(lookup, "REDIS_URL", "")}

	var err error
	c.host, c.dbName, err = parseRedisURL(c.url)

	return c, err
}

func loadLogging(lookup lookupFunc) (logging, error) {
	var errs []error

	c := logging{format: getenv(lookup, "LOG_FORMAT", "text")}
	if c.format != "text" && c.format != "json" {
		errs = append(errs, fmt.Errorf(`LOG_FORMAT must be either "text" or "json", got %q`, c.format))
	}

	rawLevel := getenv(lookup, "LOG_LEVEL", "info")
	if err := c.level.UnmarshalText([]byte(rawLevel)); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL is not valid, got %q", rawLevel))
	}

	return c, errors.Join(errs...)
}

// loadTasks reads which store backs the tasks and the settings of the overdue
// scan worker. OVERDUE_SCAN_WORKER_INTERVAL is only checked when the worker is
// enabled.
func loadTasks(lookup lookupFunc) (tasks, error) {
	var errs []error

	c := tasks{store: getenv(lookup, "TASKS_STORE", "memory")}
	if c.store != "postgres" && c.store != "memory" {
		errs = append(errs, fmt.Errorf(`TASKS_STORE must be either "memory" or "postgres", got %q`, c.store))
	}

	rawEnabled := getenv(lookup, "OVERDUE_SCAN_WORKER_ENABLED", "true")
	enabled, err := strconv.ParseBool(rawEnabled)
	if err != nil {
		errs = append(errs, fmt.Errorf(`OVERDUE_SCAN_WORKER_ENABLED must be either "true" or "false", got %q`, rawEnabled))
	}

	c.overdueScanEnabled = enabled
	c.overdueScanInterval = 30 * time.Second
	if c.overdueScanEnabled {
		rawInterval := getenv(lookup, "OVERDUE_SCAN_WORKER_INTERVAL", "30s")
		c.overdueScanInterval, err = time.ParseDuration(rawInterval)
		if err != nil || c.overdueScanInterval <= 0 {
			errs = append(errs, fmt.Errorf(`OVERDUE_SCAN_WORKER_INTERVAL must be a positive duration like "30s", "1m" or "500ms", got %q`, rawInterval))
		}
	}

	return c, errors.Join(errs...)
}

// loadRateLimit reads the rate limiter settings. RATE_LIMIT_BURST is only
// checked when rate limiting is enabled (RATE_LIMIT_RPS > 0).
func loadRateLimit(lookup lookupFunc) (rateLimit, error) {
	var errs []error

	rawRPS := getenv(lookup, "RATE_LIMIT_RPS", "10")
	rps, err := strconv.ParseFloat(rawRPS, 64)
	if err != nil || rps < 0 || math.IsNaN(rps) || math.IsInf(rps, 0) {
		errs = append(errs, fmt.Errorf(`RATE_LIMIT_RPS must be a positive float number like 0.5, 1 or set to 0 to disable rate limiting, got %q`, rawRPS))
	}

	c := rateLimit{rps: rps}
	if c.rps > 0 {
		rawBurst := getenv(lookup, "RATE_LIMIT_BURST", "20")
		c.burst, err = strconv.Atoi(rawBurst)
		if err != nil || c.burst < 1 {
			errs = append(errs, fmt.Errorf(`RATE_LIMIT_BURST must be a positive integer, got %q`, rawBurst))
		}
	}

	return c, errors.Join(errs...)
}

// loadCORS reads the CORS settings. CORS_MAX_AGE is only checked when
// CORS_ALLOWED_ORIGINS is set.
func loadCORS(lookup lookupFunc) (cors, error) {
	var errs []error

	origins, err := parseCORSOrigins(getenv(lookup, "CORS_ALLOWED_ORIGINS", ""))
	if err != nil {
		errs = append(errs, err)
	}

	c := cors{origins: origins, maxAge: 10 * time.Minute}
	if len(c.origins) > 0 {
		rawMaxAge := getenv(lookup, "CORS_MAX_AGE", "10m")
		c.maxAge, err = time.ParseDuration(rawMaxAge)
		if err != nil || c.maxAge < 0 || c.maxAge%time.Second != 0 {
			errs = append(errs, fmt.Errorf(`CORS_MAX_AGE must be a non-negative duration in whole seconds like "30s", "10m" or "1h", got %q`, rawMaxAge))
		}
	}

	return c, errors.Join(errs...)
}

// loadAddrs reads the listen addresses of the diagnostics servers. An empty
// PPROF_ADDR means there is no pprof server.
func loadAddrs(lookup lookupFunc) (addrs, error) {
	var errs []error

	c := addrs{
		pprof: getenv(lookup, "PPROF_ADDR", ""),
		ops:   getenv(lookup, "OPS_ADDR", "127.0.0.1:9091"),
	}

	if c.pprof != "" {
		errs = append(errs, checkListenAddr("PPROF_ADDR", c.pprof))
	}
	errs = append(errs, checkListenAddr("OPS_ADDR", c.ops))

	return c, errors.Join(errs...)
}

func loadJWT(lookup lookupFunc) (jwtCfg, error) {
	var errs []error

	c := jwtCfg{
		privateKeyPath: getenv(lookup, "JWT_PRIVATE_KEY_PATH", "secrets/jwt_ed25519.key"),
		publicKeyPath:  getenv(lookup, "JWT_PUBLIC_KEY_PATH", "secrets/jwt_ed25519.pub"),
		issuer:         getenv(lookup, "JWT_ISSUER", ""),
		audiences:      parseAudiences(getenv(lookup, "JWT_AUDIENCES", "")),
	}

	if c.issuer == "" {
		errs = append(errs, errors.New("JWT_ISSUER must not be empty"))
	}

	if len(c.audiences) == 0 {
		errs = append(errs, errors.New("JWT_AUDIENCES must contain at least one audience, separated by commas"))
	}

	return c, errors.Join(errs...)
}

func loadTokens(lookup lookupFunc) (tokens, error) {
	var errs []error
	var c tokens
	var err error

	if c.accessTTL, err = getPositiveDuration(lookup, "ACCESS_TTL", "10m"); err != nil {
		errs = append(errs, err)
	}

	if c.refreshTTL, err = getPositiveDuration(lookup, "REFRESH_TTL", "720h"); err != nil {
		errs = append(errs, err)
	}

	if c.refreshGrace, err = getPositiveDuration(lookup, "REFRESH_GRACE_PERIOD", "15s"); err != nil {
		errs = append(errs, err)
	}

	if c.verifyEmailTTL, err = getPositiveDuration(lookup, "VERIFY_EMAIL_TOKEN_TTL", "24h"); err != nil {
		errs = append(errs, err)
	}

	if c.resetPasswordTTL, err = getPositiveDuration(lookup, "RESET_PASSWORD_TOKEN_TTL", "1h"); err != nil {
		errs = append(errs, err)
	}

	return c, errors.Join(errs...)
}

// defaultArgon2Concurrency is how many hashes may run at once when
// ARGON2_CONCURRENCY is not set: as many as the CPUs can run in parallel with
// threads lanes each, but at least one.
func defaultArgon2Concurrency(cpus, threads int) int {
	return max(1, cpus/threads)
}

// loadArgon2 reads the Argon2id parameters. cpus only feeds the default of
// ARGON2_CONCURRENCY.
func loadArgon2(lookup lookupFunc, cpus int) (argon2, error) {
	var errs []error
	var c argon2

	passes, err := getUint(lookup, "ARGON2_TIME", "3", 1, math.MaxUint32)
	if err != nil {
		errs = append(errs, err)
	}
	c.time = uint32(passes)

	threads, threadsErr := getUint(lookup, "ARGON2_THREADS", "4", 1, math.MaxUint8)
	if threadsErr != nil {
		errs = append(errs, threadsErr)
	}
	c.threads = uint8(threads)

	// Argon2 needs at least 8 KiB per lane. Without a valid thread count, only
	// the generic lower bound can be checked.
	minMemory := uint64(8)
	if threadsErr == nil {
		minMemory = 8 * threads
	}

	memory, err := getUint(lookup, "ARGON2_MEMORY_KIB", "65536", minMemory, math.MaxUint32)
	if err != nil {
		errs = append(errs, err)
	}
	c.memoryKiB = uint32(memory)

	defaultConcurrency := 1
	if threadsErr == nil {
		defaultConcurrency = defaultArgon2Concurrency(cpus, int(threads))
	}

	concurrency, err := getUint(lookup, "ARGON2_CONCURRENCY", strconv.Itoa(defaultConcurrency), 1, math.MaxInt32)
	if err != nil {
		errs = append(errs, err)
	}
	c.concurrency = int(concurrency)

	return c, errors.Join(errs...)
}

// getMillisDuration reads a Go duration of at least one millisecond that is a
// whole number of milliseconds, using fallback when the variable is unset. The
// Redis limiter scripts count in milliseconds, so anything finer would be
// rounded down, possibly to zero.
func getMillisDuration(lookup lookupFunc, key, fallback string) (time.Duration, error) {
	raw := getenv(lookup, key, fallback)
	d, err := parseMillisDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration in whole milliseconds, at least 1ms, like \"500ms\", \"30s\" or \"1h\", got %q", key, raw)
	}
	return d, nil
}

func parseMillisDuration(raw string) (time.Duration, error) {
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Millisecond || d%time.Millisecond != 0 {
		return 0, errors.New("invalid duration")
	}
	return d, nil
}

// loadLogin reads the settings of the login limiter. LOGIN_WINDOW must cover the
// longest backoff, otherwise the failed-attempt counter would expire before the
// last lock does and the backoff could never reach its final step.
func loadLogin(lookup lookupFunc) (loginCfg, error) {
	var errs []error
	var c loginCfg
	var err error

	free, err := getUint(lookup, "LOGIN_FREE_ATTEMPTS", "4", 1, math.MaxInt32)
	if err != nil {
		errs = append(errs, err)
	}
	c.freeAttempts = int(free)

	rawBackoff := getenv(lookup, "LOGIN_BACKOFF", "30s,1m,5m,15m")
	for entry := range strings.SplitSeq(rawBackoff, ",") {
		d, err := parseMillisDuration(strings.TrimSpace(entry))
		if err != nil {
			errs = append(errs, fmt.Errorf("LOGIN_BACKOFF must be a comma-separated list of durations in whole milliseconds, at least 1ms, like \"30s,1m,5m\", got %q", rawBackoff))
			c.backoff = nil
			break
		}
		c.backoff = append(c.backoff, d)
	}

	c.window, err = getMillisDuration(lookup, "LOGIN_WINDOW", "1h")
	if err != nil {
		errs = append(errs, err)
	} else if len(c.backoff) > 0 && c.window < slices.Max(c.backoff) {
		errs = append(errs, fmt.Errorf("LOGIN_WINDOW (%s) must be at least the longest LOGIN_BACKOFF step (%s)", c.window, slices.Max(c.backoff)))
	}

	return c, errors.Join(errs...)
}

// loadMail reads the settings of the limiter on emails sent on request
// (verification and password reset).
func loadMail(lookup lookupFunc) (mailCfg, error) {
	var errs []error
	var c mailCfg
	var err error

	if c.requestCooldown, err = getMillisDuration(lookup, "MAIL_REQUEST_COOLDOWN", "60s"); err != nil {
		errs = append(errs, err)
	}

	hourlyMax, err := getUint(lookup, "MAIL_REQUEST_HOURLY_MAX", "5", 1, math.MaxInt32)
	if err != nil {
		errs = append(errs, err)
	}
	c.requestHourlyMax = int(hourlyMax)

	return c, errors.Join(errs...)
}

// loadSMTP reads the settings of the SMTP server that sends the emails. Host and
// sender are required, the rest have defaults. Username and password go together:
// without them the server is used without authentication.
func loadSMTP(lookup lookupFunc) (email.SMTPConfig, error) {
	var errs []error
	var c email.SMTPConfig

	c.Host = getenv(lookup, "SMTP_HOST", "")
	if c.Host == "" {
		errs = append(errs, errors.New("SMTP_HOST must not be empty"))
	}

	rawPort := getenv(lookup, "SMTP_PORT", "587")
	port, ok := isValidPort(rawPort)
	if !ok {
		errs = append(errs, fmt.Errorf("SMTP_PORT must be an integer between 1 and 65535, got %q", rawPort))
	}
	c.Port = port

	c.TLS = email.TLSMode(getenv(lookup, "SMTP_TLS_MODE", string(email.TLSStartTLS)))
	switch c.TLS {
	case email.TLSStartTLS, email.TLSOpportunistic, email.TLSNone:
	default:
		errs = append(errs, fmt.Errorf(`SMTP_TLS_MODE must be "starttls", "opportunistic" or "none", got %q`, c.TLS))
	}

	var err error
	if c.Timeout, err = getPositiveDuration(lookup, "SMTP_TIMEOUT", "10s"); err != nil {
		errs = append(errs, err)
	}

	// Credentials are not trimmed: a password may start or end with spaces.
	c.Username = lookup("SMTP_USERNAME")
	c.Password = lookup("SMTP_PASSWORD")
	if (c.Username == "") != (c.Password == "") {
		errs = append(errs, errors.New("SMTP_USERNAME and SMTP_PASSWORD must be set together or both left empty"))
	}

	c.From = getenv(lookup, "SMTP_FROM", "")
	if c.From == "" {
		errs = append(errs, errors.New("SMTP_FROM must not be empty"))
	} else if _, err := mail.ParseAddress(c.From); err != nil {
		errs = append(errs, fmt.Errorf(`SMTP_FROM must look like "taskapi@example.com" or "Task API <taskapi@example.com>", got %q`, c.From))
	}

	return c, errors.Join(errs...)
}

// loadOutbox reads the settings of the email outbox worker. OUTBOX_RETRY_CAP must
// be at least OUTBOX_RETRY_BASE, otherwise the backoff could not even reach its
// first step. The lease must
// outlast the worst case of a batch, where every email takes the full send
// timeout one after the other, otherwise another worker could claim items that
// are still being sent and the same email would go out twice. The constraint is
// only checked when the three values are valid on their own.
func loadOutbox(lookup lookupFunc) (outboxCfg, error) {
	var errs []error
	var c outboxCfg
	var err error

	leaseOK, batchOK, timeoutOK := true, true, true

	if c.lease, err = getPositiveDuration(lookup, "OUTBOX_LEASE", "5m"); err != nil {
		errs = append(errs, err)
		leaseOK = false
	}

	batchSize, err := getUint(lookup, "OUTBOX_BATCH_SIZE", "5", 1, math.MaxInt32)
	if err != nil {
		errs = append(errs, err)
		batchOK = false
	}
	c.batchSize = int(batchSize)

	if c.sendTimeout, err = getPositiveDuration(lookup, "OUTBOX_SEND_TIMEOUT", "30s"); err != nil {
		errs = append(errs, err)
		timeoutOK = false
	}

	if c.pollInterval, err = getPositiveDuration(lookup, "OUTBOX_POLL_INTERVAL", "30s"); err != nil {
		errs = append(errs, err)
	}

	maxAttempts, err := getUint(lookup, "OUTBOX_MAX_ATTEMPTS", "6", 1, math.MaxInt32)
	if err != nil {
		errs = append(errs, err)
	}
	c.maxAttempts = int(maxAttempts)

	baseOK, capOK := true, true

	if c.retryBase, err = getPositiveDuration(lookup, "OUTBOX_RETRY_BASE", "1m"); err != nil {
		errs = append(errs, err)
		baseOK = false
	}

	if c.retryCap, err = getPositiveDuration(lookup, "OUTBOX_RETRY_CAP", "15m"); err != nil {
		errs = append(errs, err)
		capOK = false
	}

	if baseOK && capOK && c.retryCap < c.retryBase {
		errs = append(errs, fmt.Errorf("OUTBOX_RETRY_CAP (%s) must be at least OUTBOX_RETRY_BASE (%s)", c.retryCap, c.retryBase))
	}

	if c.sentRetention, err = getPositiveDuration(lookup, "OUTBOX_SENT_RETENTION", "168h"); err != nil {
		errs = append(errs, err)
	}

	if leaseOK && batchOK && timeoutOK {
		batch := int64(c.batchSize)
		// a product that overflows is certainly longer than the lease
		if batch > math.MaxInt64/int64(c.sendTimeout) || time.Duration(batch)*c.sendTimeout >= c.lease {
			errs = append(errs, fmt.Errorf("OUTBOX_LEASE (%s) must be longer than OUTBOX_BATCH_SIZE * OUTBOX_SEND_TIMEOUT (%d * %s)", c.lease, c.batchSize, c.sendTimeout))
		}
	}

	return c, errors.Join(errs...)
}

// loadCleanup reads the settings of the cleanup job.
func loadCleanup(lookup lookupFunc) (cleanupCfg, error) {
	var c cleanupCfg
	var err error

	c.interval, err = getPositiveDuration(lookup, "CLEANUP_INTERVAL", "1h")

	return c, err
}

// loadAppBaseURL reads APP_BASE_URL, the origin that the links in verification
// and password-reset emails are built on. It must be a bare http(s)://host[:port]
// origin, which is normalized the same way as the CORS origins.
func loadAppBaseURL(lookup lookupFunc) (string, error) {
	raw := getenv(lookup, "APP_BASE_URL", "http://localhost:5173")

	u, err := url.Parse(raw)
	if err != nil ||
		(u.Scheme != "http" && u.Scheme != "https") ||
		u.Host == "" || u.Hostname() == "" ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" ||
		strings.HasSuffix(raw, "?") || strings.HasSuffix(raw, "#") {
		return "", fmt.Errorf(`APP_BASE_URL must look like "https://app.example.com" or "http://localhost:5173" (no path or trailing slash), got %q`, raw)
	}

	if port := u.Port(); port != "" {
		if _, ok := isValidPort(port); !ok {
			return "", fmt.Errorf("APP_BASE_URL has an invalid port, got %q", raw)
		}
	}

	return normalizeOrigin(u), nil
}

// loadConfig reads and validates every environment variable. All invalid values
// are reported together in the returned error.
func loadConfig(lookup lookupFunc) (config, error) {
	var errs []error
	var c config

	rawPort := getenv(lookup, "PORT", "8080")
	port, ok := isValidPort(rawPort)
	if !ok {
		errs = append(errs, fmt.Errorf("PORT must be an integer between 1 and 65535, got %q", rawPort))
	}
	c.port = port

	// errors.Join drops nil errors, so each result is appended as is.
	var err error

	c.tasks, err = loadTasks(lookup)
	errs = append(errs, err)

	c.db, err = loadDB(lookup)
	errs = append(errs, err)

	c.redis, err = loadRedis(lookup)
	errs = append(errs, err)

	c.logging, err = loadLogging(lookup)
	errs = append(errs, err)

	c.rateLimit, err = loadRateLimit(lookup)
	errs = append(errs, err)

	c.cors, err = loadCORS(lookup)
	errs = append(errs, err)

	c.addrs, err = loadAddrs(lookup)
	errs = append(errs, err)

	c.jwt, err = loadJWT(lookup)
	errs = append(errs, err)

	c.tokens, err = loadTokens(lookup)
	errs = append(errs, err)

	c.argon2, err = loadArgon2(lookup, runtime.GOMAXPROCS(0))
	errs = append(errs, err)

	c.login, err = loadLogin(lookup)
	errs = append(errs, err)

	c.mail, err = loadMail(lookup)
	errs = append(errs, err)

	c.smtp, err = loadSMTP(lookup)
	errs = append(errs, err)

	c.outbox, err = loadOutbox(lookup)
	errs = append(errs, err)

	c.cleanup, err = loadCleanup(lookup)
	errs = append(errs, err)

	c.appBaseURL, err = loadAppBaseURL(lookup)
	errs = append(errs, err)

	return c, errors.Join(errs...)
}
