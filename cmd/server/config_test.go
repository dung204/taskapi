package main

import (
	"errors"
	"log/slog"
	"maps"
	"math"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dung204/taskapi/internal/email"
)

// testRedisURL, testDBURL, testIssuer, testAudience, testSMTPHost and
// testSMTPFrom are the REDIS_URL, DATABASE_URL, JWT_ISSUER, JWT_AUDIENCES,
// SMTP_HOST and SMTP_FROM every TestLoadConfig case starts
// with, because loadConfig requires valid ones. A case
// overrides them by setting the variable.
const (
	testRedisURL = "redis://localhost:6379/0"
	testDBURL    = "postgres://u:p@localhost:5432/tasks"
	testIssuer   = "taskapi"
	testAudience = "taskapi-clients"
	testSMTPHost = "smtp.example.com"
	testSMTPFrom = "Task API <taskapi@example.com>"
)

// testCPUs is the CPU count TestLoadArgon2 pretends to have, so the default
// ARGON2_CONCURRENCY does not depend on the machine running the tests.
const testCPUs = 8

// loadCase is one case of a table test of a load* function. env is merged over
// the base environment given to checkLoad; an empty value counts as unset. A
// case either has wantErr, the strings the error must contain, or want.
type loadCase[T any] struct {
	name    string
	env     map[string]string
	want    T
	wantErr []string
}

// checkLoad runs the cases against load. The group structs have unexported
// fields, so they are compared with cmp.Exporter.
func checkLoad[T any](t *testing.T, load func(lookupFunc) (T, error), base map[string]string, tests []loadCase[T]) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := maps.Clone(base)
			if env == nil {
				env = map[string]string{}
			}
			maps.Copy(env, tt.env)

			got, err := load(func(k string) string { return env[k] })
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("load() error = nil, want error containing %q", tt.wantErr)
				}
				for _, s := range tt.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("load() error = %q, want it to contain %q", err, s)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("load() unexpected error: %v", err)
			}

			if diff := cmp.Diff(tt.want, got, cmp.Exporter(func(reflect.Type) bool { return true })); diff != "" {
				t.Errorf("load() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLoadDB(t *testing.T) {
	checkLoad(t, loadDB, nil, []loadCase[dbCfg]{
		{name: "valid DATABASE_URL", env: map[string]string{"DATABASE_URL": testDBURL}, want: dbCfg{url: testDBURL, host: "localhost", name: "tasks"}},
		{name: "host and name are extracted", env: map[string]string{"DATABASE_URL": "postgres://u:p@host:5432/db"}, want: dbCfg{url: "postgres://u:p@host:5432/db", host: "host", name: "db"}},
		{name: "missing DATABASE_URL", wantErr: []string{"DATABASE_URL"}},
		{name: "invalid DATABASE_URL", env: map[string]string{"DATABASE_URL": "abcd1234"}, wantErr: []string{"DATABASE_URL"}},
	})
}

func TestLoadRedis(t *testing.T) {
	checkLoad(t, loadRedis, nil, []loadCase[redisCfg]{
		{name: "valid REDIS_URL", env: map[string]string{"REDIS_URL": testRedisURL}, want: redisCfg{url: testRedisURL, host: "localhost", dbName: "0"}},
		{name: "host and database are extracted", env: map[string]string{"REDIS_URL": "redis://u:p@cache.internal:6380/3"}, want: redisCfg{url: "redis://u:p@cache.internal:6380/3", host: "cache.internal", dbName: "3"}},
		{name: "missing REDIS_URL", wantErr: []string{"REDIS_URL"}},
		{name: "invalid REDIS_URL", env: map[string]string{"REDIS_URL": "abcd1234"}, wantErr: []string{"REDIS_URL"}},
		{name: "REDIS_URL with wrong scheme", env: map[string]string{"REDIS_URL": "postgres://localhost:6379/0"}, wantErr: []string{"REDIS_URL"}},
	})
}

func TestLoadLogging(t *testing.T) {
	checkLoad(t, loadLogging, nil, []loadCase[loggingCfg]{
		{name: "defaults", want: loggingCfg{format: "text", level: slog.LevelInfo}},
		{name: "valid LOG_FORMAT", env: map[string]string{"LOG_FORMAT": "json"}, want: loggingCfg{format: "json", level: slog.LevelInfo}},
		{name: "valid LOG_LEVEL", env: map[string]string{"LOG_LEVEL": "debug"}, want: loggingCfg{format: "text", level: slog.LevelDebug}},
		{name: "invalid LOG_FORMAT", env: map[string]string{"LOG_FORMAT": "yaml"}, wantErr: []string{"LOG_FORMAT", "yaml"}},
		{name: "invalid LOG_LEVEL", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: []string{"LOG_LEVEL", "loud"}},
		{name: "both invalid are reported together", env: map[string]string{"LOG_FORMAT": "yaml", "LOG_LEVEL": "loud"}, wantErr: []string{"LOG_FORMAT", "yaml", "LOG_LEVEL", "loud"}},
	})
}

func TestLoadTasks(t *testing.T) {
	defaults := tasksCfg{overdueScanEnabled: true, overdueScanInterval: 30 * time.Second}
	with := func(mod func(*tasksCfg)) tasksCfg {
		c := defaults
		mod(&c)
		return c
	}

	checkLoad(t, loadTasks, nil, []loadCase[tasksCfg]{
		{name: "defaults", want: defaults},
		{name: "valid OVERDUE_SCAN_WORKER_ENABLED", env: map[string]string{"OVERDUE_SCAN_WORKER_ENABLED": "false"}, want: with(func(c *tasksCfg) { c.overdueScanEnabled = false })},
		{name: "OVERDUE_SCAN_WORKER_ENABLED=true with valid OVERDUE_SCAN_WORKER_INTERVAL", env: map[string]string{"OVERDUE_SCAN_WORKER_ENABLED": "true", "OVERDUE_SCAN_WORKER_INTERVAL": "500ms"}, want: with(func(c *tasksCfg) { c.overdueScanInterval = 500 * time.Millisecond })},
		{name: "OVERDUE_SCAN_WORKER_ENABLED=false with invalid OVERDUE_SCAN_WORKER_INTERVAL", env: map[string]string{"OVERDUE_SCAN_WORKER_ENABLED": "false", "OVERDUE_SCAN_WORKER_INTERVAL": "abc"}, want: with(func(c *tasksCfg) { c.overdueScanEnabled = false })},
		{name: "invalid OVERDUE_SCAN_WORKER_ENABLED", env: map[string]string{"OVERDUE_SCAN_WORKER_ENABLED": "maybe"}, wantErr: []string{"OVERDUE_SCAN_WORKER_ENABLED", "maybe"}},
		{name: "OVERDUE_SCAN_WORKER_ENABLED=true with non-duration OVERDUE_SCAN_WORKER_INTERVAL", env: map[string]string{"OVERDUE_SCAN_WORKER_ENABLED": "true", "OVERDUE_SCAN_WORKER_INTERVAL": "abc"}, wantErr: []string{"OVERDUE_SCAN_WORKER_INTERVAL", "abc"}},
		{name: "OVERDUE_SCAN_WORKER_ENABLED=true with zero OVERDUE_SCAN_WORKER_INTERVAL", env: map[string]string{"OVERDUE_SCAN_WORKER_ENABLED": "true", "OVERDUE_SCAN_WORKER_INTERVAL": "0s"}, wantErr: []string{"OVERDUE_SCAN_WORKER_INTERVAL", "0s"}},
		{name: "OVERDUE_SCAN_WORKER_ENABLED=true with negative OVERDUE_SCAN_WORKER_INTERVAL", env: map[string]string{"OVERDUE_SCAN_WORKER_ENABLED": "true", "OVERDUE_SCAN_WORKER_INTERVAL": "-1s"}, wantErr: []string{"OVERDUE_SCAN_WORKER_INTERVAL", "-1s"}},
		{name: "the old WORKER_ variables are ignored", env: map[string]string{"WORKER_ENABLED": "false", "WORKER_INTERVAL": "abc"}, want: defaults},
	})
}

func TestLoadRateLimit(t *testing.T) {
	checkLoad(t, loadRateLimit, nil, []loadCase[rateLimitCfg]{
		{name: "defaults", want: rateLimitCfg{rps: 10, burst: 20}},
		{name: "valid RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "0.5"}, want: rateLimitCfg{rps: 0.5, burst: 20}},
		{name: "valid RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "5"}, want: rateLimitCfg{rps: 10, burst: 5}},
		{name: "RATE_LIMIT_RPS=0 disables rate limiting", env: map[string]string{"RATE_LIMIT_RPS": "0"}, want: rateLimitCfg{}},
		{name: "RATE_LIMIT_RPS=0 with invalid RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_RPS": "0", "RATE_LIMIT_BURST": "abc"}, want: rateLimitCfg{}},
		{name: "non-numerical RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "abc"}, wantErr: []string{"RATE_LIMIT_RPS", "abc"}},
		{name: "negative RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "-1"}, wantErr: []string{"RATE_LIMIT_RPS", "-1"}},
		{name: "NaN RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "NaN"}, wantErr: []string{"RATE_LIMIT_RPS", "NaN"}},
		{name: "Inf RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "Inf"}, wantErr: []string{"RATE_LIMIT_RPS", "Inf"}},
		{name: "-Inf RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "-Inf"}, wantErr: []string{"RATE_LIMIT_RPS", "-Inf"}},
		{name: "non-numerical RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "abc"}, wantErr: []string{"RATE_LIMIT_BURST", "abc"}},
		{name: "fractional RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "1.5"}, wantErr: []string{"RATE_LIMIT_BURST", "1.5"}},
		{name: "zero RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "0"}, wantErr: []string{"RATE_LIMIT_BURST", "0"}},
		{name: "negative RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "-5"}, wantErr: []string{"RATE_LIMIT_BURST", "-5"}},
	})
}

func TestLoadCORS(t *testing.T) {
	checkLoad(t, loadCORS, nil, []loadCase[corsCfg]{
		{name: "defaults", want: corsCfg{maxAge: 10 * time.Minute}},
		{name: "valid CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com"}, want: corsCfg{origins: []string{"https://app.example.com"}, maxAge: 10 * time.Minute}},
		{name: "multiple CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com, http://localhost:5173"}, want: corsCfg{origins: []string{"https://app.example.com", "http://localhost:5173"}, maxAge: 10 * time.Minute}},
		{name: "wildcard CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*"}, want: corsCfg{origins: []string{"*"}, maxAge: 10 * time.Minute}},
		{name: "valid CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "1h"}, want: corsCfg{origins: []string{"*"}, maxAge: time.Hour}},
		{name: "zero CORS_MAX_AGE omits the header", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "0"}, want: corsCfg{origins: []string{"*"}}},
		{name: "CORS_MAX_AGE is not checked without CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_MAX_AGE": "abc"}, want: corsCfg{maxAge: 10 * time.Minute}},
		{name: "invalid CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_ALLOWED_ORIGINS": "app.example.com"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "app.example.com"}},
		{name: "wildcard mixed with origins", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*,https://app.example.com"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "*"}},
		{name: "non-duration CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "abc"}, wantErr: []string{"CORS_MAX_AGE", "abc"}},
		{name: "negative CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "-1s"}, wantErr: []string{"CORS_MAX_AGE", "-1s"}},
		{name: "sub-second CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "1500ms"}, wantErr: []string{"CORS_MAX_AGE", "1500ms"}},
	})
}

func TestLoadAddrs(t *testing.T) {
	checkLoad(t, loadAddrs, nil, []loadCase[addrsCfg]{
		{name: "defaults", want: addrsCfg{ops: "127.0.0.1:9091"}},
		{name: "valid PPROF_ADDR", env: map[string]string{"PPROF_ADDR": ":6060"}, want: addrsCfg{pprof: ":6060", ops: "127.0.0.1:9091"}},
		{name: "empty PPROF_ADDR", env: map[string]string{"PPROF_ADDR": ""}, want: addrsCfg{ops: "127.0.0.1:9091"}},
		{name: "valid OPS_ADDR", env: map[string]string{"OPS_ADDR": "localhost:9090"}, want: addrsCfg{ops: "localhost:9090"}},
		{name: "invalid PPROF_ADDR", env: map[string]string{"PPROF_ADDR": "6060"}, wantErr: []string{"PPROF_ADDR", "6060"}},
		{name: "invalid OPS_ADDR", env: map[string]string{"OPS_ADDR": "9090"}, wantErr: []string{"OPS_ADDR", "9090"}},
		{name: "both invalid are reported together", env: map[string]string{"PPROF_ADDR": "6060", "OPS_ADDR": "9090"}, wantErr: []string{"PPROF_ADDR", "6060", "OPS_ADDR", "9090"}},
	})
}

func TestLoadJWT(t *testing.T) {
	base := map[string]string{"JWT_ISSUER": testIssuer, "JWT_AUDIENCES": testAudience}
	defaults := jwtCfg{
		privateKeyPath: "secrets/jwt_ed25519.key",
		publicKeyPath:  "secrets/jwt_ed25519.pub",
		issuer:         testIssuer,
		audiences:      []string{testAudience},
	}
	with := func(mod func(*jwtCfg)) jwtCfg {
		c := defaults
		mod(&c)
		return c
	}

	checkLoad(t, loadJWT, base, []loadCase[jwtCfg]{
		{name: "defaults", want: defaults},
		{
			name: "custom key paths",
			env:  map[string]string{"JWT_PRIVATE_KEY_PATH": "/run/secrets/jwt.key", "JWT_PUBLIC_KEY_PATH": "/run/secrets/jwt.pub"},
			want: with(func(c *jwtCfg) {
				c.privateKeyPath = "/run/secrets/jwt.key"
				c.publicKeyPath = "/run/secrets/jwt.pub"
			}),
		},
		{name: "valid JWT_ISSUER", env: map[string]string{"JWT_ISSUER": "https://api.example.com"}, want: with(func(c *jwtCfg) { c.issuer = "https://api.example.com" })},
		{name: "multiple JWT_AUDIENCES", env: map[string]string{"JWT_AUDIENCES": "https://api.example.com, taskapi-clients"}, want: with(func(c *jwtCfg) { c.audiences = []string{"https://api.example.com", "taskapi-clients"} })},
		{name: "JWT_AUDIENCES ignores empty entries", env: map[string]string{"JWT_AUDIENCES": ",a,, b ,"}, want: with(func(c *jwtCfg) { c.audiences = []string{"a", "b"} })},
		{name: "JWT_AUDIENCES drops duplicates", env: map[string]string{"JWT_AUDIENCES": "a,b,a"}, want: with(func(c *jwtCfg) { c.audiences = []string{"a", "b"} })},
		{name: "missing JWT_ISSUER", env: map[string]string{"JWT_ISSUER": ""}, wantErr: []string{"JWT_ISSUER"}},
		{name: "missing JWT_AUDIENCES", env: map[string]string{"JWT_AUDIENCES": ""}, wantErr: []string{"JWT_AUDIENCES"}},
		{name: "JWT_AUDIENCES with only separators", env: map[string]string{"JWT_AUDIENCES": " , ,"}, wantErr: []string{"JWT_AUDIENCES"}},
		{name: "both missing are reported together", env: map[string]string{"JWT_ISSUER": "", "JWT_AUDIENCES": ""}, wantErr: []string{"JWT_ISSUER", "JWT_AUDIENCES"}},
	})
}

func TestLoadTokens(t *testing.T) {
	defaults := tokensCfg{
		accessTTL:        10 * time.Minute,
		refreshTTL:       720 * time.Hour,
		refreshGrace:     15 * time.Second,
		verifyEmailTTL:   24 * time.Hour,
		resetPasswordTTL: time.Hour,
	}
	with := func(mod func(*tokensCfg)) tokensCfg {
		c := defaults
		mod(&c)
		return c
	}

	checkLoad(t, loadTokens, nil, []loadCase[tokensCfg]{
		{name: "defaults", want: defaults},
		{name: "valid ACCESS_TTL", env: map[string]string{"ACCESS_TTL": "5m"}, want: with(func(c *tokensCfg) { c.accessTTL = 5 * time.Minute })},
		{name: "valid REFRESH_TTL", env: map[string]string{"REFRESH_TTL": "24h"}, want: with(func(c *tokensCfg) { c.refreshTTL = 24 * time.Hour })},
		{name: "valid VERIFY_EMAIL_TOKEN_TTL", env: map[string]string{"VERIFY_EMAIL_TOKEN_TTL": "48h"}, want: with(func(c *tokensCfg) { c.verifyEmailTTL = 48 * time.Hour })},
		{name: "valid RESET_PASSWORD_TOKEN_TTL", env: map[string]string{"RESET_PASSWORD_TOKEN_TTL": "15m"}, want: with(func(c *tokensCfg) { c.resetPasswordTTL = 15 * time.Minute })},
		{name: "non-duration ACCESS_TTL", env: map[string]string{"ACCESS_TTL": "abc"}, wantErr: []string{"ACCESS_TTL", "abc"}},
		{name: "zero ACCESS_TTL", env: map[string]string{"ACCESS_TTL": "0s"}, wantErr: []string{"ACCESS_TTL", "0s"}},
		{name: "negative ACCESS_TTL", env: map[string]string{"ACCESS_TTL": "-5m"}, wantErr: []string{"ACCESS_TTL", "-5m"}},
		{name: "REFRESH_TTL in days is not a Go duration", env: map[string]string{"REFRESH_TTL": "30d"}, wantErr: []string{"REFRESH_TTL", "30d"}},
		{name: "valid REFRESH_GRACE_PERIOD", env: map[string]string{"REFRESH_GRACE_PERIOD": "30s"}, want: with(func(c *tokensCfg) { c.refreshGrace = 30 * time.Second })},
		{name: "zero REFRESH_GRACE_PERIOD", env: map[string]string{"REFRESH_GRACE_PERIOD": "0"}, wantErr: []string{"REFRESH_GRACE_PERIOD", "0"}},
		{name: "zero REFRESH_TTL", env: map[string]string{"REFRESH_TTL": "0"}, wantErr: []string{"REFRESH_TTL", "0"}},
		{name: "negative REFRESH_TTL", env: map[string]string{"REFRESH_TTL": "-1h"}, wantErr: []string{"REFRESH_TTL", "-1h"}},
		{name: "non-duration VERIFY_EMAIL_TOKEN_TTL", env: map[string]string{"VERIFY_EMAIL_TOKEN_TTL": "1d"}, wantErr: []string{"VERIFY_EMAIL_TOKEN_TTL", "1d"}},
		{name: "zero VERIFY_EMAIL_TOKEN_TTL", env: map[string]string{"VERIFY_EMAIL_TOKEN_TTL": "0"}, wantErr: []string{"VERIFY_EMAIL_TOKEN_TTL", "0"}},
		{name: "non-duration RESET_PASSWORD_TOKEN_TTL", env: map[string]string{"RESET_PASSWORD_TOKEN_TTL": "abc"}, wantErr: []string{"RESET_PASSWORD_TOKEN_TTL", "abc"}},
		{name: "negative RESET_PASSWORD_TOKEN_TTL", env: map[string]string{"RESET_PASSWORD_TOKEN_TTL": "-1h"}, wantErr: []string{"RESET_PASSWORD_TOKEN_TTL", "-1h"}},
		{name: "all invalid are reported together", env: map[string]string{"ACCESS_TTL": "abc", "REFRESH_TTL": "30d", "VERIFY_EMAIL_TOKEN_TTL": "x", "RESET_PASSWORD_TOKEN_TTL": "y"}, wantErr: []string{"ACCESS_TTL", "REFRESH_TTL", "VERIFY_EMAIL_TOKEN_TTL", "RESET_PASSWORD_TOKEN_TTL"}},
	})
}

func TestLoadArgon2(t *testing.T) {
	defaults := argon2Cfg{time: 3, memoryKiB: 65536, threads: 4, concurrency: defaultArgon2Concurrency(testCPUs, 4)}
	with := func(mod func(*argon2Cfg)) argon2Cfg {
		c := defaults
		mod(&c)
		return c
	}

	load := func(lookup lookupFunc) (argon2Cfg, error) { return loadArgon2(lookup, testCPUs) }

	checkLoad(t, load, nil, []loadCase[argon2Cfg]{
		{name: "defaults", want: defaults},
		{name: "valid ARGON2_TIME", env: map[string]string{"ARGON2_TIME": "1"}, want: with(func(c *argon2Cfg) { c.time = 1 })},
		{name: "valid ARGON2_MEMORY_KIB", env: map[string]string{"ARGON2_MEMORY_KIB": "19456"}, want: with(func(c *argon2Cfg) { c.memoryKiB = 19456 })},
		{
			name: "valid ARGON2_THREADS changes the default ARGON2_CONCURRENCY",
			env:  map[string]string{"ARGON2_THREADS": "2"},
			want: with(func(c *argon2Cfg) {
				c.threads = 2
				c.concurrency = defaultArgon2Concurrency(testCPUs, 2)
			}),
		},
		{name: "valid ARGON2_CONCURRENCY", env: map[string]string{"ARGON2_CONCURRENCY": "3"}, want: with(func(c *argon2Cfg) { c.concurrency = 3 })},
		{name: "ARGON2_MEMORY_KIB at the minimum for the threads", env: map[string]string{"ARGON2_THREADS": "4", "ARGON2_MEMORY_KIB": "32"}, want: with(func(c *argon2Cfg) { c.memoryKiB = 32 })},
		{name: "max ARGON2_THREADS", env: map[string]string{"ARGON2_THREADS": "255"}, want: with(func(c *argon2Cfg) { c.threads = math.MaxUint8; c.concurrency = 1 })},
		{name: "non-numerical ARGON2_TIME", env: map[string]string{"ARGON2_TIME": "abc"}, wantErr: []string{"ARGON2_TIME", "abc"}},
		{name: "zero ARGON2_TIME", env: map[string]string{"ARGON2_TIME": "0"}, wantErr: []string{"ARGON2_TIME", "0"}},
		{name: "negative ARGON2_TIME", env: map[string]string{"ARGON2_TIME": "-1"}, wantErr: []string{"ARGON2_TIME", "-1"}},
		{name: "ARGON2_TIME out of range", env: map[string]string{"ARGON2_TIME": "4294967296"}, wantErr: []string{"ARGON2_TIME", "4294967296"}},
		{name: "zero ARGON2_THREADS", env: map[string]string{"ARGON2_THREADS": "0"}, wantErr: []string{"ARGON2_THREADS", "0"}},
		{name: "ARGON2_THREADS out of range", env: map[string]string{"ARGON2_THREADS": "256"}, wantErr: []string{"ARGON2_THREADS", "256"}},
		{name: "fractional ARGON2_THREADS", env: map[string]string{"ARGON2_THREADS": "1.5"}, wantErr: []string{"ARGON2_THREADS", "1.5"}},
		{name: "ARGON2_MEMORY_KIB below 8 KiB per thread", env: map[string]string{"ARGON2_THREADS": "4", "ARGON2_MEMORY_KIB": "31"}, wantErr: []string{"ARGON2_MEMORY_KIB", "31"}},
		{name: "ARGON2_MEMORY_KIB out of range", env: map[string]string{"ARGON2_MEMORY_KIB": "4294967296"}, wantErr: []string{"ARGON2_MEMORY_KIB", "4294967296"}},
		{name: "zero ARGON2_CONCURRENCY", env: map[string]string{"ARGON2_CONCURRENCY": "0"}, wantErr: []string{"ARGON2_CONCURRENCY", "0"}},
		{name: "negative ARGON2_CONCURRENCY", env: map[string]string{"ARGON2_CONCURRENCY": "-2"}, wantErr: []string{"ARGON2_CONCURRENCY", "-2"}},
		{name: "invalid ARGON2_THREADS does not hide other errors", env: map[string]string{"ARGON2_THREADS": "abc", "ARGON2_TIME": "0", "ARGON2_CONCURRENCY": "0"}, wantErr: []string{"ARGON2_THREADS", "ARGON2_TIME", "ARGON2_CONCURRENCY"}},
	})
}

func TestLoadLogin(t *testing.T) {
	defaults := loginCfg{
		freeAttempts: 4,
		backoff:      []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute},
		window:       time.Hour,
	}
	with := func(mod func(*loginCfg)) loginCfg {
		c := defaults
		mod(&c)
		return c
	}

	checkLoad(t, loadLogin, nil, []loadCase[loginCfg]{
		{name: "defaults", want: defaults},
		{name: "valid LOGIN_FREE_ATTEMPTS", env: map[string]string{"LOGIN_FREE_ATTEMPTS": "1"}, want: with(func(c *loginCfg) { c.freeAttempts = 1 })},
		{name: "valid LOGIN_BACKOFF", env: map[string]string{"LOGIN_BACKOFF": "1s, 2s ,500ms"}, want: with(func(c *loginCfg) {
			c.backoff = []time.Duration{time.Second, 2 * time.Second, 500 * time.Millisecond}
		})},
		{name: "single LOGIN_BACKOFF step", env: map[string]string{"LOGIN_BACKOFF": "10s"}, want: with(func(c *loginCfg) { c.backoff = []time.Duration{10 * time.Second} })},
		{name: "valid LOGIN_WINDOW", env: map[string]string{"LOGIN_WINDOW": "24h"}, want: with(func(c *loginCfg) { c.window = 24 * time.Hour })},
		{name: "LOGIN_WINDOW equal to the longest step", env: map[string]string{"LOGIN_WINDOW": "15m"}, want: with(func(c *loginCfg) { c.window = 15 * time.Minute })},
		{name: "zero LOGIN_FREE_ATTEMPTS", env: map[string]string{"LOGIN_FREE_ATTEMPTS": "0"}, wantErr: []string{"LOGIN_FREE_ATTEMPTS", "0"}},
		{name: "non-numerical LOGIN_FREE_ATTEMPTS", env: map[string]string{"LOGIN_FREE_ATTEMPTS": "abc"}, wantErr: []string{"LOGIN_FREE_ATTEMPTS", "abc"}},
		{name: "negative LOGIN_FREE_ATTEMPTS", env: map[string]string{"LOGIN_FREE_ATTEMPTS": "-1"}, wantErr: []string{"LOGIN_FREE_ATTEMPTS", "-1"}},
		{name: "non-duration LOGIN_BACKOFF step", env: map[string]string{"LOGIN_BACKOFF": "30s,abc"}, wantErr: []string{"LOGIN_BACKOFF", "30s,abc"}},
		{name: "empty LOGIN_BACKOFF step", env: map[string]string{"LOGIN_BACKOFF": "30s,,1m"}, wantErr: []string{"LOGIN_BACKOFF", "30s,,1m"}},
		{name: "zero LOGIN_BACKOFF step", env: map[string]string{"LOGIN_BACKOFF": "30s,0s"}, wantErr: []string{"LOGIN_BACKOFF", "30s,0s"}},
		{name: "negative LOGIN_BACKOFF step", env: map[string]string{"LOGIN_BACKOFF": "-30s"}, wantErr: []string{"LOGIN_BACKOFF", "-30s"}},
		{name: "LOGIN_BACKOFF step finer than a millisecond", env: map[string]string{"LOGIN_BACKOFF": "1500us"}, wantErr: []string{"LOGIN_BACKOFF", "1500us"}},
		{name: "zero LOGIN_WINDOW", env: map[string]string{"LOGIN_WINDOW": "0"}, wantErr: []string{"LOGIN_WINDOW", "0"}},
		{name: "non-duration LOGIN_WINDOW", env: map[string]string{"LOGIN_WINDOW": "1d"}, wantErr: []string{"LOGIN_WINDOW", "1d"}},
		{name: "LOGIN_WINDOW shorter than the longest step", env: map[string]string{"LOGIN_WINDOW": "10m"}, wantErr: []string{"LOGIN_WINDOW", "10m0s", "15m0s"}},
		{name: "all invalid are reported together", env: map[string]string{"LOGIN_FREE_ATTEMPTS": "0", "LOGIN_BACKOFF": "x", "LOGIN_WINDOW": "y"}, wantErr: []string{"LOGIN_FREE_ATTEMPTS", "LOGIN_BACKOFF", "LOGIN_WINDOW"}},
	})
}

func TestLoadAuthMail(t *testing.T) {
	defaults := authMailCfg{requestCooldown: time.Minute, requestHourlyMax: 5}

	checkLoad(t, loadAuthMail, nil, []loadCase[authMailCfg]{
		{name: "defaults", want: defaults},
		{name: "valid AUTH_MAIL_COOLDOWN", env: map[string]string{"AUTH_MAIL_COOLDOWN": "90s"}, want: authMailCfg{requestCooldown: 90 * time.Second, requestHourlyMax: 5}},
		{name: "valid AUTH_MAIL_HOURLY_MAX", env: map[string]string{"AUTH_MAIL_HOURLY_MAX": "20"}, want: authMailCfg{requestCooldown: time.Minute, requestHourlyMax: 20}},
		{name: "non-duration AUTH_MAIL_COOLDOWN", env: map[string]string{"AUTH_MAIL_COOLDOWN": "abc"}, wantErr: []string{"AUTH_MAIL_COOLDOWN", "abc"}},
		{name: "zero AUTH_MAIL_COOLDOWN", env: map[string]string{"AUTH_MAIL_COOLDOWN": "0"}, wantErr: []string{"AUTH_MAIL_COOLDOWN", "0"}},
		{name: "negative AUTH_MAIL_COOLDOWN", env: map[string]string{"AUTH_MAIL_COOLDOWN": "-1m"}, wantErr: []string{"AUTH_MAIL_COOLDOWN", "-1m"}},
		{name: "AUTH_MAIL_COOLDOWN finer than a millisecond", env: map[string]string{"AUTH_MAIL_COOLDOWN": "1500us"}, wantErr: []string{"AUTH_MAIL_COOLDOWN", "1500us"}},
		{name: "zero AUTH_MAIL_HOURLY_MAX", env: map[string]string{"AUTH_MAIL_HOURLY_MAX": "0"}, wantErr: []string{"AUTH_MAIL_HOURLY_MAX", "0"}},
		{name: "non-numerical AUTH_MAIL_HOURLY_MAX", env: map[string]string{"AUTH_MAIL_HOURLY_MAX": "many"}, wantErr: []string{"AUTH_MAIL_HOURLY_MAX", "many"}},
		{name: "both invalid are reported together", env: map[string]string{"AUTH_MAIL_COOLDOWN": "abc", "AUTH_MAIL_HOURLY_MAX": "0"}, wantErr: []string{"AUTH_MAIL_COOLDOWN", "AUTH_MAIL_HOURLY_MAX"}},
	})
}

func TestLoadInviteMail(t *testing.T) {
	defaults := inviteMailCfg{requestCooldown: 10 * time.Second, requestHourlyMax: 5}

	checkLoad(t, loadInviteMail, nil, []loadCase[inviteMailCfg]{
		{name: "defaults", want: defaults},
		{name: "valid INVITE_MAIL_COOLDOWN", env: map[string]string{"INVITE_MAIL_COOLDOWN": "90s"}, want: inviteMailCfg{requestCooldown: 90 * time.Second, requestHourlyMax: 5}},
		{name: "valid INVITE_MAIL_HOURLY_MAX", env: map[string]string{"INVITE_MAIL_HOURLY_MAX": "20"}, want: inviteMailCfg{requestCooldown: 10 * time.Second, requestHourlyMax: 20}},
		{name: "zero INVITE_MAIL_COOLDOWN", env: map[string]string{"INVITE_MAIL_COOLDOWN": "0"}, wantErr: []string{"INVITE_MAIL_COOLDOWN", "0"}},
		{name: "INVITE_MAIL_COOLDOWN finer than a millisecond", env: map[string]string{"INVITE_MAIL_COOLDOWN": "1500us"}, wantErr: []string{"INVITE_MAIL_COOLDOWN", "1500us"}},
		{name: "zero INVITE_MAIL_HOURLY_MAX", env: map[string]string{"INVITE_MAIL_HOURLY_MAX": "0"}, wantErr: []string{"INVITE_MAIL_HOURLY_MAX", "0"}},
		{name: "both invalid are reported together", env: map[string]string{"INVITE_MAIL_COOLDOWN": "abc", "INVITE_MAIL_HOURLY_MAX": "0"}, wantErr: []string{"INVITE_MAIL_COOLDOWN", "INVITE_MAIL_HOURLY_MAX"}},
	})
}

func TestLoadOutbox(t *testing.T) {
	defaults := outboxCfg{lease: 5 * time.Minute, batchSize: 5, sendTimeout: 30 * time.Second, pollInterval: 30 * time.Second, maxAttempts: 6, retryBase: time.Minute, retryCap: 15 * time.Minute, sentRetention: 168 * time.Hour}
	with := func(mod func(*outboxCfg)) outboxCfg {
		c := defaults
		mod(&c)
		return c
	}

	checkLoad(t, loadOutbox, nil, []loadCase[outboxCfg]{
		{name: "defaults", want: defaults},
		{name: "valid OUTBOX_LEASE", env: map[string]string{"OUTBOX_LEASE": "10m"}, want: with(func(c *outboxCfg) { c.lease = 10 * time.Minute })},
		{name: "valid OUTBOX_BATCH_SIZE", env: map[string]string{"OUTBOX_BATCH_SIZE": "9"}, want: with(func(c *outboxCfg) { c.batchSize = 9 })},
		{name: "valid OUTBOX_SEND_TIMEOUT", env: map[string]string{"OUTBOX_SEND_TIMEOUT": "10s"}, want: with(func(c *outboxCfg) { c.sendTimeout = 10 * time.Second })},
		{
			name: "lease just above batch * timeout",
			env:  map[string]string{"OUTBOX_LEASE": "151s"},
			want: with(func(c *outboxCfg) { c.lease = 151 * time.Second }),
		},
		{name: "lease equal to batch * timeout", env: map[string]string{"OUTBOX_LEASE": "150s"}, wantErr: []string{"OUTBOX_LEASE", "OUTBOX_BATCH_SIZE", "OUTBOX_SEND_TIMEOUT"}},
		{name: "lease shorter than batch * timeout", env: map[string]string{"OUTBOX_BATCH_SIZE": "20"}, wantErr: []string{"OUTBOX_LEASE", "20"}},
		{name: "batch * timeout overflows", env: map[string]string{"OUTBOX_BATCH_SIZE": "2147483647", "OUTBOX_SEND_TIMEOUT": "2562047h"}, wantErr: []string{"OUTBOX_LEASE"}},
		{name: "valid OUTBOX_POLL_INTERVAL", env: map[string]string{"OUTBOX_POLL_INTERVAL": "5s"}, want: with(func(c *outboxCfg) { c.pollInterval = 5 * time.Second })},
		{name: "OUTBOX_POLL_INTERVAL is independent of the lease constraint", env: map[string]string{"OUTBOX_POLL_INTERVAL": "1h"}, want: with(func(c *outboxCfg) { c.pollInterval = time.Hour })},
		{name: "non-duration OUTBOX_POLL_INTERVAL", env: map[string]string{"OUTBOX_POLL_INTERVAL": "abc"}, wantErr: []string{"OUTBOX_POLL_INTERVAL", "abc"}},
		{name: "zero OUTBOX_POLL_INTERVAL", env: map[string]string{"OUTBOX_POLL_INTERVAL": "0"}, wantErr: []string{"OUTBOX_POLL_INTERVAL", "0"}},
		{name: "negative OUTBOX_POLL_INTERVAL", env: map[string]string{"OUTBOX_POLL_INTERVAL": "-1s"}, wantErr: []string{"OUTBOX_POLL_INTERVAL", "-1s"}},
		{name: "valid OUTBOX_MAX_ATTEMPTS", env: map[string]string{"OUTBOX_MAX_ATTEMPTS": "10"}, want: with(func(c *outboxCfg) { c.maxAttempts = 10 })},
		{name: "OUTBOX_MAX_ATTEMPTS of 1 means no retry", env: map[string]string{"OUTBOX_MAX_ATTEMPTS": "1"}, want: with(func(c *outboxCfg) { c.maxAttempts = 1 })},
		{name: "valid OUTBOX_RETRY_BASE", env: map[string]string{"OUTBOX_RETRY_BASE": "10s"}, want: with(func(c *outboxCfg) { c.retryBase = 10 * time.Second })},
		{name: "valid OUTBOX_RETRY_CAP", env: map[string]string{"OUTBOX_RETRY_CAP": "1h"}, want: with(func(c *outboxCfg) { c.retryCap = time.Hour })},
		{name: "OUTBOX_RETRY_CAP equal to OUTBOX_RETRY_BASE", env: map[string]string{"OUTBOX_RETRY_BASE": "15m"}, want: with(func(c *outboxCfg) { c.retryBase = 15 * time.Minute })},
		{name: "OUTBOX_RETRY_CAP below OUTBOX_RETRY_BASE", env: map[string]string{"OUTBOX_RETRY_BASE": "16m"}, wantErr: []string{"OUTBOX_RETRY_CAP", "OUTBOX_RETRY_BASE", "16m"}},
		{name: "OUTBOX_RETRY_CAP lowered below the default base", env: map[string]string{"OUTBOX_RETRY_CAP": "30s"}, wantErr: []string{"OUTBOX_RETRY_CAP", "30s"}},
		{name: "zero OUTBOX_MAX_ATTEMPTS", env: map[string]string{"OUTBOX_MAX_ATTEMPTS": "0"}, wantErr: []string{"OUTBOX_MAX_ATTEMPTS", "0"}},
		{name: "non-numerical OUTBOX_MAX_ATTEMPTS", env: map[string]string{"OUTBOX_MAX_ATTEMPTS": "few"}, wantErr: []string{"OUTBOX_MAX_ATTEMPTS", "few"}},
		{name: "non-duration OUTBOX_RETRY_BASE", env: map[string]string{"OUTBOX_RETRY_BASE": "abc"}, wantErr: []string{"OUTBOX_RETRY_BASE", "abc"}},
		{name: "zero OUTBOX_RETRY_BASE", env: map[string]string{"OUTBOX_RETRY_BASE": "0"}, wantErr: []string{"OUTBOX_RETRY_BASE", "0"}},
		{name: "negative OUTBOX_RETRY_CAP", env: map[string]string{"OUTBOX_RETRY_CAP": "-1m"}, wantErr: []string{"OUTBOX_RETRY_CAP", "-1m"}},
		{name: "invalid base does not also report the cap constraint", env: map[string]string{"OUTBOX_RETRY_BASE": "abc"}, wantErr: []string{"OUTBOX_RETRY_BASE"}},
		{name: "valid OUTBOX_SENT_RETENTION", env: map[string]string{"OUTBOX_SENT_RETENTION": "24h"}, want: with(func(c *outboxCfg) { c.sentRetention = 24 * time.Hour })},
		{name: "non-duration OUTBOX_SENT_RETENTION", env: map[string]string{"OUTBOX_SENT_RETENTION": "7d"}, wantErr: []string{"OUTBOX_SENT_RETENTION", "7d"}},
		{name: "zero OUTBOX_SENT_RETENTION", env: map[string]string{"OUTBOX_SENT_RETENTION": "0"}, wantErr: []string{"OUTBOX_SENT_RETENTION", "0"}},
		{name: "negative OUTBOX_SENT_RETENTION", env: map[string]string{"OUTBOX_SENT_RETENTION": "-24h"}, wantErr: []string{"OUTBOX_SENT_RETENTION", "-24h"}},
		{name: "non-duration OUTBOX_LEASE", env: map[string]string{"OUTBOX_LEASE": "abc"}, wantErr: []string{"OUTBOX_LEASE", "abc"}},
		{name: "zero OUTBOX_LEASE", env: map[string]string{"OUTBOX_LEASE": "0"}, wantErr: []string{"OUTBOX_LEASE", "0"}},
		{name: "zero OUTBOX_BATCH_SIZE", env: map[string]string{"OUTBOX_BATCH_SIZE": "0"}, wantErr: []string{"OUTBOX_BATCH_SIZE", "0"}},
		{name: "non-numerical OUTBOX_BATCH_SIZE", env: map[string]string{"OUTBOX_BATCH_SIZE": "many"}, wantErr: []string{"OUTBOX_BATCH_SIZE", "many"}},
		{name: "negative OUTBOX_SEND_TIMEOUT", env: map[string]string{"OUTBOX_SEND_TIMEOUT": "-5s"}, wantErr: []string{"OUTBOX_SEND_TIMEOUT", "-5s"}},
		{name: "all invalid are reported together", env: map[string]string{"OUTBOX_LEASE": "x", "OUTBOX_BATCH_SIZE": "0", "OUTBOX_SEND_TIMEOUT": "y", "OUTBOX_POLL_INTERVAL": "z", "OUTBOX_MAX_ATTEMPTS": "0", "OUTBOX_RETRY_BASE": "u", "OUTBOX_RETRY_CAP": "v", "OUTBOX_SENT_RETENTION": "w"}, wantErr: []string{"OUTBOX_SENT_RETENTION", "OUTBOX_LEASE", "OUTBOX_BATCH_SIZE", "OUTBOX_SEND_TIMEOUT", "OUTBOX_POLL_INTERVAL", "OUTBOX_MAX_ATTEMPTS", "OUTBOX_RETRY_BASE", "OUTBOX_RETRY_CAP"}},
	})
}

func TestLoadCleanup(t *testing.T) {
	checkLoad(t, loadCleanup, nil, []loadCase[cleanupCfg]{
		{name: "default", want: cleanupCfg{interval: time.Hour}},
		{name: "valid CLEANUP_INTERVAL", env: map[string]string{"CLEANUP_INTERVAL": "15m"}, want: cleanupCfg{interval: 15 * time.Minute}},
		{name: "non-duration CLEANUP_INTERVAL", env: map[string]string{"CLEANUP_INTERVAL": "1d"}, wantErr: []string{"CLEANUP_INTERVAL", "1d"}},
		{name: "zero CLEANUP_INTERVAL", env: map[string]string{"CLEANUP_INTERVAL": "0"}, wantErr: []string{"CLEANUP_INTERVAL", "0"}},
		{name: "negative CLEANUP_INTERVAL", env: map[string]string{"CLEANUP_INTERVAL": "-1h"}, wantErr: []string{"CLEANUP_INTERVAL", "-1h"}},
	})
}

func TestLoadAppBaseURL(t *testing.T) {
	checkLoad(t, loadAppBaseURL, nil, []loadCase[string]{
		{name: "default", want: "http://localhost:5173"},
		{name: "valid https origin", env: map[string]string{"APP_BASE_URL": "https://app.example.com"}, want: "https://app.example.com"},
		{name: "normalized", env: map[string]string{"APP_BASE_URL": "HTTPS://App.Example.com:443"}, want: "https://app.example.com"},
		{name: "trailing slash", env: map[string]string{"APP_BASE_URL": "https://app.example.com/"}, wantErr: []string{"APP_BASE_URL", "https://app.example.com/"}},
		{name: "with path", env: map[string]string{"APP_BASE_URL": "https://example.com/app"}, wantErr: []string{"APP_BASE_URL"}},
		{name: "no scheme", env: map[string]string{"APP_BASE_URL": "app.example.com"}, wantErr: []string{"APP_BASE_URL"}},
		{name: "unsupported scheme", env: map[string]string{"APP_BASE_URL": "ftp://example.com"}, wantErr: []string{"APP_BASE_URL"}},
		{name: "invalid port", env: map[string]string{"APP_BASE_URL": "http://localhost:99999"}, wantErr: []string{"APP_BASE_URL", "port"}},
	})
}

func TestLoadSMTP(t *testing.T) {
	base := map[string]string{"SMTP_HOST": testSMTPHost, "SMTP_FROM": testSMTPFrom}
	defaults := email.SMTPConfig{
		Host:    testSMTPHost,
		Port:    587,
		TLS:     email.TLSStartTLS,
		Timeout: 10 * time.Second,
		From:    testSMTPFrom,
	}
	with := func(mod func(*email.SMTPConfig)) email.SMTPConfig {
		c := defaults
		mod(&c)
		return c
	}

	checkLoad(t, loadSMTP, base, []loadCase[email.SMTPConfig]{
		{name: "defaults", want: defaults},
		{name: "valid SMTP_HOST", env: map[string]string{"SMTP_HOST": "localhost"}, want: with(func(c *email.SMTPConfig) { c.Host = "localhost" })},
		{name: "valid SMTP_PORT", env: map[string]string{"SMTP_PORT": "1025"}, want: with(func(c *email.SMTPConfig) { c.Port = 1025 })},
		{name: "SMTP_TLS_MODE=opportunistic", env: map[string]string{"SMTP_TLS_MODE": "opportunistic"}, want: with(func(c *email.SMTPConfig) { c.TLS = email.TLSOpportunistic })},
		{name: "SMTP_TLS_MODE=none", env: map[string]string{"SMTP_TLS_MODE": "none"}, want: with(func(c *email.SMTPConfig) { c.TLS = email.TLSNone })},
		{name: "valid SMTP_TIMEOUT", env: map[string]string{"SMTP_TIMEOUT": "500ms"}, want: with(func(c *email.SMTPConfig) { c.Timeout = 500 * time.Millisecond })},
		{
			name: "SMTP_USERNAME and SMTP_PASSWORD together",
			env:  map[string]string{"SMTP_USERNAME": "user", "SMTP_PASSWORD": " p@ss "},
			want: with(func(c *email.SMTPConfig) {
				c.Username = "user"
				c.Password = " p@ss "
			}),
		},
		{name: "SMTP_FROM with only an address", env: map[string]string{"SMTP_FROM": "taskapi@example.com"}, want: with(func(c *email.SMTPConfig) { c.From = "taskapi@example.com" })},
		{name: "missing SMTP_HOST", env: map[string]string{"SMTP_HOST": ""}, wantErr: []string{"SMTP_HOST"}},
		{name: "non-numerical SMTP_PORT", env: map[string]string{"SMTP_PORT": "abc"}, wantErr: []string{"SMTP_PORT", "abc"}},
		{name: "zero SMTP_PORT", env: map[string]string{"SMTP_PORT": "0"}, wantErr: []string{"SMTP_PORT", "0"}},
		{name: "SMTP_PORT out of range", env: map[string]string{"SMTP_PORT": "65536"}, wantErr: []string{"SMTP_PORT", "65536"}},
		{name: "invalid SMTP_TLS_MODE", env: map[string]string{"SMTP_TLS_MODE": "ssl"}, wantErr: []string{"SMTP_TLS_MODE", "ssl"}},
		{name: "non-duration SMTP_TIMEOUT", env: map[string]string{"SMTP_TIMEOUT": "abc"}, wantErr: []string{"SMTP_TIMEOUT", "abc"}},
		{name: "zero SMTP_TIMEOUT", env: map[string]string{"SMTP_TIMEOUT": "0s"}, wantErr: []string{"SMTP_TIMEOUT", "0s"}},
		{name: "negative SMTP_TIMEOUT", env: map[string]string{"SMTP_TIMEOUT": "-5s"}, wantErr: []string{"SMTP_TIMEOUT", "-5s"}},
		{name: "SMTP_USERNAME without SMTP_PASSWORD", env: map[string]string{"SMTP_USERNAME": "user"}, wantErr: []string{"SMTP_USERNAME", "SMTP_PASSWORD"}},
		{name: "SMTP_PASSWORD without SMTP_USERNAME", env: map[string]string{"SMTP_PASSWORD": "secret"}, wantErr: []string{"SMTP_USERNAME", "SMTP_PASSWORD"}},
		{name: "missing SMTP_FROM", env: map[string]string{"SMTP_FROM": ""}, wantErr: []string{"SMTP_FROM"}},
		{name: "invalid SMTP_FROM", env: map[string]string{"SMTP_FROM": "not an address"}, wantErr: []string{"SMTP_FROM", "not an address"}},
		{name: "all invalid are reported together", env: map[string]string{"SMTP_HOST": "", "SMTP_PORT": "x", "SMTP_TLS_MODE": "y", "SMTP_TIMEOUT": "z", "SMTP_FROM": ""}, wantErr: []string{"SMTP_HOST", "SMTP_PORT", "SMTP_TLS_MODE", "SMTP_TIMEOUT", "SMTP_FROM"}},
	})
}

func defaultConfig() config {
	return config{
		port:      8080,
		tasks:     tasksCfg{overdueScanEnabled: true, overdueScanInterval: 30 * time.Second},
		db:        dbCfg{url: testDBURL, host: "localhost", name: "tasks"},
		redis:     redisCfg{url: testRedisURL, host: "localhost", dbName: "0"},
		logging:   loggingCfg{format: "text", level: slog.LevelInfo},
		rateLimit: rateLimitCfg{rps: 10, burst: 20},
		cors:      corsCfg{maxAge: 10 * time.Minute},
		addrs:     addrsCfg{ops: "127.0.0.1:9091"},
		jwt: jwtCfg{
			privateKeyPath: "secrets/jwt_ed25519.key",
			publicKeyPath:  "secrets/jwt_ed25519.pub",
			issuer:         testIssuer,
			audiences:      []string{testAudience},
		},
		tokens: tokensCfg{accessTTL: 10 * time.Minute, refreshTTL: 720 * time.Hour, refreshGrace: 15 * time.Second, verifyEmailTTL: 24 * time.Hour, resetPasswordTTL: time.Hour},
		argon2: argon2Cfg{
			time:        3,
			memoryKiB:   65536,
			threads:     4,
			concurrency: defaultArgon2Concurrency(runtime.GOMAXPROCS(0), 4),
		},
		login: loginCfg{
			freeAttempts: 4,
			backoff:      []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute},
			window:       time.Hour,
		},
		authMail:    authMailCfg{requestCooldown: time.Minute, requestHourlyMax: 5},
		inviteMail:  inviteMailCfg{requestCooldown: 10 * time.Second, requestHourlyMax: 5},
		inviteOwner: inviteOwnerCfg{hourlyMax: 30},
		smtp: email.SMTPConfig{
			Host:    testSMTPHost,
			Port:    587,
			TLS:     email.TLSStartTLS,
			Timeout: 10 * time.Second,
			From:    testSMTPFrom,
		},
		outbox:     outboxCfg{lease: 5 * time.Minute, batchSize: 5, sendTimeout: 30 * time.Second, pollInterval: 30 * time.Second, maxAttempts: 6, retryBase: time.Minute, retryCap: 15 * time.Minute, sentRetention: 168 * time.Hour},
		cleanup:    cleanupCfg{interval: time.Hour},
		appBaseURL: "http://localhost:5173",
	}
}

func cfgWith(mod func(*config)) config {
	c := defaultConfig()
	mod(&c)
	return c
}

// TestLoadConfig covers what loadConfig does itself: PORT, which is outside any
// group, treating empty values as unset, wiring every group's loader, and
// reporting all errors together. Each group's own rules are tested in its
// TestLoad* function.
func TestLoadConfig(t *testing.T) {
	base := map[string]string{
		"REDIS_URL":     testRedisURL,
		"DATABASE_URL":  testDBURL,
		"JWT_ISSUER":    testIssuer,
		"JWT_AUDIENCES": testAudience,
		"SMTP_HOST":     testSMTPHost,
		"SMTP_FROM":     testSMTPFrom,
	}

	checkLoad(t, loadConfig, base, []loadCase[config]{
		{name: "defaults", want: defaultConfig()},
		{name: "field with empty value", env: map[string]string{"PORT": "", "LOG_FORMAT": "json"}, want: cfgWith(func(c *config) { c.logging.format = "json" })},

		{name: "valid PORT", env: map[string]string{"PORT": "9000"}, want: cfgWith(func(c *config) { c.port = 9000 })},
		{name: "invalid PORT", env: map[string]string{"PORT": "abc"}, wantErr: []string{"PORT", "abc"}},
		{name: "PORT out of range", env: map[string]string{"PORT": "65536"}, wantErr: []string{"PORT", "65536"}},

		{
			name:    "invalid DATABASE_URL",
			env:     map[string]string{"DATABASE_URL": "abcd1234"},
			wantErr: []string{"DATABASE_URL"},
		},
		{
			name:    "missing DATABASE_URL",
			env:     map[string]string{"DATABASE_URL": ""},
			wantErr: []string{"DATABASE_URL"},
		},

		{
			name: "every group is loaded",
			env: map[string]string{
				"DATABASE_URL":                "postgres://u:p@host:5432/db",
				"REDIS_URL":                   "redis://cache.internal:6380/3",
				"LOG_FORMAT":                  "json",
				"OVERDUE_SCAN_WORKER_ENABLED": "false",
				"RATE_LIMIT_RPS":              "0.5",
				"CORS_ALLOWED_ORIGINS":        "*",
				"PPROF_ADDR":                  ":6060",
				"JWT_ISSUER":                  "https://api.example.com",
				"ACCESS_TTL":                  "5m",
				"ARGON2_TIME":                 "1",
			},
			want: cfgWith(func(c *config) {
				c.db = dbCfg{url: "postgres://u:p@host:5432/db", host: "host", name: "db"}
				c.redis = redisCfg{url: "redis://cache.internal:6380/3", host: "cache.internal", dbName: "3"}
				c.logging.format = "json"
				c.tasks.overdueScanEnabled = false
				c.rateLimit.rps = 0.5
				c.cors.origins = []string{"*"}
				c.addrs.pprof = ":6060"
				c.jwt.issuer = "https://api.example.com"
				c.tokens.accessTTL = 5 * time.Minute
				c.argon2.time = 1
			}),
		},
		{
			name: "errors of every group are reported together",
			env: map[string]string{
				"PORT":                         "xyz",
				"DATABASE_URL":                 "abc",
				"REDIS_URL":                    "abc",
				"LOG_LEVEL":                    "loud",
				"OVERDUE_SCAN_WORKER_INTERVAL": "abc",
				"RATE_LIMIT_BURST":             "abc",
				"CORS_ALLOWED_ORIGINS":         "app.example.com",
				"OPS_ADDR":                     "9090",
				"JWT_ISSUER":                   "",
				"ACCESS_TTL":                   "abc",
				"ARGON2_TIME":                  "0",
			},
			wantErr: []string{
				"PORT", "xyz", "DATABASE_URL", "REDIS_URL", "LOG_LEVEL", "loud", "OVERDUE_SCAN_WORKER_INTERVAL",
				"RATE_LIMIT_BURST", "CORS_ALLOWED_ORIGINS", "OPS_ADDR", "JWT_ISSUER", "ACCESS_TTL", "ARGON2_TIME",
			},
		},
	})
}

func TestIsValidPort(t *testing.T) {
	tests := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"1", 1, true},
		{"65535", 65535, true},
		{"65536", 0, false},
		{"0", 0, false},
		{"-1", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, gotOK := isValidPort(tt.in)

			if tt.want != got {
				t.Errorf("isValidPort(%q) port = %d, want %d", tt.in, got, tt.want)
			}

			if tt.wantOK != gotOK {
				t.Errorf("isValidPort(%q) ok = %t, want %t", tt.in, gotOK, tt.wantOK)
			}
		})
	}
}

func TestCheckListenAddr(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "port only", value: ":6060"},
		{name: "hostname and port", value: "localhost:6060"},
		{name: "IPv4 and port", value: "127.0.0.1:6060"},
		{name: "IPv6 and port", value: "[::1]:6060"},
		{name: "max port", value: "0.0.0.0:65535"},

		{name: "empty value", value: "", wantErr: true},
		{name: "missing colon", value: "6060", wantErr: true},
		{name: "missing port", value: "localhost", wantErr: true},
		{name: "empty port", value: "localhost:", wantErr: true},
		{name: "too many colons", value: "localhost:6060:1", wantErr: true},
		{name: "URL instead of address", value: "http://localhost:6060", wantErr: true},
		{name: "non-numerical port", value: ":abc", wantErr: true},
		{name: "port zero", value: ":0", wantErr: true},
		{name: "negative port", value: ":-1", wantErr: true},
		{name: "port out of range", value: ":65536", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkListenAddr("PPROF_ADDR", tt.value)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("checkListenAddr(%q) unexpected error: %v", tt.value, err)
				}
				return
			}

			if err == nil {
				t.Fatalf("checkListenAddr(%q) error = nil, want error", tt.value)
			}

			for _, s := range []string{"PPROF_ADDR", tt.value} {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("checkListenAddr(%q) error = %q, want it to contain %q", tt.value, err, s)
				}
			}
		})
	}
}

func TestParseCORSOrigins(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr []string
	}{
		{name: "empty value", raw: "", want: nil},
		{name: "only separators and spaces", raw: " , ,, ", want: nil},
		{name: "single origin", raw: "https://app.example.com", want: []string{"https://app.example.com"}},
		{name: "origin with port", raw: "http://localhost:5173", want: []string{"http://localhost:5173"}},
		{name: "IPv6 origin", raw: "http://[::1]:5173", want: []string{"http://[::1]:5173"}},
		{name: "multiple origins keep their order", raw: "https://b.example.com,http://localhost:5173,https://a.example.com", want: []string{"https://b.example.com", "http://localhost:5173", "https://a.example.com"}},
		{name: "spaces and empty entries are ignored", raw: " https://a.example.com , ,https://b.example.com,", want: []string{"https://a.example.com", "https://b.example.com"}},
		{name: "wildcard", raw: "*", want: []string{"*"}},
		{name: "origins are normalized", raw: "HTTPS://App.Example.com:443", want: []string{"https://app.example.com"}},
		{name: "duplicates after normalization are dropped", raw: "https://a.example.com,HTTPS://A.example.com:443,https://a.example.com", want: []string{"https://a.example.com"}},
		{name: "same host with different schemes is kept", raw: "http://a.example.com,https://a.example.com", want: []string{"http://a.example.com", "https://a.example.com"}},

		{name: "wildcard with other origins", raw: "*,https://app.example.com", wantErr: []string{"CORS_ALLOWED_ORIGINS", "*"}},
		{name: "wildcard subdomain", raw: "https://*.example.com", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://*.example.com"}},
		{name: "wildcard with a separator", raw: "*,", wantErr: []string{"CORS_ALLOWED_ORIGINS", "*"}},
		{name: "missing scheme", raw: "app.example.com", wantErr: []string{"CORS_ALLOWED_ORIGINS", "app.example.com"}},
		{name: "unsupported scheme", raw: "ftp://app.example.com", wantErr: []string{"CORS_ALLOWED_ORIGINS", "ftp://app.example.com"}},
		{name: "missing host", raw: "https://", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://"}},
		{name: "port without host", raw: "https://:8080", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://:8080"}},
		{name: "trailing slash", raw: "https://app.example.com/", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com/"}},
		{name: "path", raw: "https://app.example.com/app", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com/app"}},
		{name: "query", raw: "https://app.example.com?a=b", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com?a=b"}},
		{name: "empty query", raw: "https://app.example.com?", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com?"}},
		{name: "fragment", raw: "https://app.example.com#top", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com#top"}},
		{name: "empty fragment", raw: "https://app.example.com#", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com#"}},
		{name: "credentials", raw: "https://u:p@app.example.com", wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://u:p@app.example.com"}},
		{name: "port out of range", raw: "https://app.example.com:65536", wantErr: []string{"CORS_ALLOWED_ORIGINS", "port", "65536"}},
		{name: "port zero", raw: "https://app.example.com:0", wantErr: []string{"CORS_ALLOWED_ORIGINS", "port", ":0"}},
		{name: "unparseable URL", raw: "https://app.example.com:abc", wantErr: []string{"CORS_ALLOWED_ORIGINS", "abc"}},
		{name: "one bad entry fails the whole list", raw: "https://a.example.com,b.example.com", wantErr: []string{"CORS_ALLOWED_ORIGINS", "b.example.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCORSOrigins(tt.raw)

			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("parseCORSOrigins(%q) error = nil, want error containing %q", tt.raw, tt.wantErr)
				}
				for _, s := range tt.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("parseCORSOrigins(%q) error = %q, want it to contain %q", tt.raw, err, s)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("parseCORSOrigins(%q) unexpected error: %v", tt.raw, err)
			}

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseCORSOrigins(%q) mismatch (-want +got):\n%s", tt.raw, diff)
			}
		})
	}
}

func TestNormalizeOrigin(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "already normalized", in: "https://app.example.com", want: "https://app.example.com"},
		{name: "uppercase scheme", in: "HTTPS://app.example.com", want: "https://app.example.com"},
		{name: "uppercase host", in: "https://App.EXAMPLE.com", want: "https://app.example.com"},
		{name: "default https port is dropped", in: "https://app.example.com:443", want: "https://app.example.com"},
		{name: "default http port is dropped", in: "http://app.example.com:80", want: "http://app.example.com"},
		{name: "non-default port is kept", in: "https://app.example.com:8443", want: "https://app.example.com:8443"},
		{name: "http port on https is kept", in: "https://app.example.com:80", want: "https://app.example.com:80"},
		{name: "https port on http is kept", in: "http://app.example.com:443", want: "http://app.example.com:443"},
		{name: "IPv6 host", in: "http://[::1]:5173", want: "http://[::1]:5173"},
		{name: "IPv6 host with default port", in: "https://[::1]:443", want: "https://[::1]"},
		{name: "port suffix inside the host is not trimmed", in: "http://host8080:8080", want: "http://host8080:8080"},
		{name: "host ending in the default port digits", in: "http://example.com:5080", want: "http://example.com:5080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.in)
			if err != nil {
				t.Fatalf("url.Parse(%q): %v", tt.in, err)
			}

			if got := normalizeOrigin(u); got != tt.want {
				t.Errorf("normalizeOrigin(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseRedisURL(t *testing.T) {
	tests := []struct {
		name       string
		redisURL   string
		wantHost   string
		wantDBName string
		wantErr    bool
	}{
		{name: "valid URL", redisURL: "redis://localhost:6379/0", wantHost: "localhost", wantDBName: "0"},
		{name: "with credentials", redisURL: "redis://u:p@cache.internal:6379/2", wantHost: "cache.internal", wantDBName: "2"},
		{name: "password only", redisURL: "redis://:secret@localhost:6379/1", wantHost: "localhost", wantDBName: "1"},
		{name: "missing port", redisURL: "redis://localhost/0", wantHost: "localhost", wantDBName: "0"},
		{name: "IPv6 host", redisURL: "redis://[::1]:6379/0", wantHost: "::1", wantDBName: "0"},

		{name: "empty URL", redisURL: "", wantErr: true},
		{name: "not a URL", redisURL: "abcd1234", wantErr: true},
		{name: "unparseable URL", redisURL: "redis://localhost:abc/0", wantErr: true},
		{name: "wrong scheme", redisURL: "postgres://localhost:6379/0", wantErr: true},
		{name: "TLS scheme is not accepted", redisURL: "rediss://localhost:6379/0", wantErr: true},
		{name: "port zero", redisURL: "redis://localhost:0/0", wantErr: true},
		{name: "port out of range", redisURL: "redis://localhost:65536/0", wantErr: true},
		{name: "missing database", redisURL: "redis://localhost:6379", wantErr: true},
		{name: "empty database", redisURL: "redis://localhost:6379/", wantErr: true},
		{name: "database contains slash", redisURL: "redis://localhost:6379/0/1", wantErr: true},
		{name: "database contains slash in the end", redisURL: "redis://localhost:6379/0/", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotDBName, gotErr := parseRedisURL(tt.redisURL)

			if tt.wantErr {
				if gotErr == nil {
					t.Fatalf("parseRedisURL(%q) error = nil, want error %q", tt.redisURL, errRedisURL)
				}

				if !errors.Is(gotErr, errRedisURL) {
					t.Fatalf("parseRedisURL(%q) unexpected error: %v", tt.redisURL, gotErr)
				}
				return
			}

			if gotErr != nil {
				t.Fatalf("parseRedisURL(%q) unexpected error: %v", tt.redisURL, gotErr)
			}

			if tt.wantHost != gotHost {
				t.Errorf("parseRedisURL(%q) host = %q, want %q", tt.redisURL, gotHost, tt.wantHost)
			}

			if tt.wantDBName != gotDBName {
				t.Errorf("parseRedisURL(%q) dbName = %q, want %q", tt.redisURL, gotDBName, tt.wantDBName)
			}
		})
	}
}

func TestParseDBURL(t *testing.T) {
	tests := []struct {
		name       string
		dbURL      string
		wantHost   string
		wantDBName string
		wantErr    bool
	}{
		{name: "valid URL", dbURL: "postgres://u:p@localhost:5432/tasks", wantHost: "localhost", wantDBName: "tasks"},
		{name: "postgresql scheme", dbURL: "postgresql://u:p@localhost:5432/tasks", wantHost: "localhost", wantDBName: "tasks"},
		{name: "unparseable URL", dbURL: "postgres://u:p@localhost:abc/tasks", wantErr: true},
		{name: "wrong scheme", dbURL: "https://u:p@localhost:5432/tasks", wantErr: true},
		{name: "missing host", dbURL: "postgres://u:p@:5432/tasks", wantErr: true},
		{name: "missing port", dbURL: "postgres://u:p@localhost/tasks", wantHost: "localhost", wantDBName: "tasks"},
		{name: "port out of range", dbURL: "postgres://u:p@localhost:65536/tasks", wantErr: true},
		{name: "database name contains slash", dbURL: "postgres://u:p@localhost:5432/tasks/prod", wantErr: true},
		{name: "database name contains slash in the end", dbURL: "postgres://u:p@localhost:5432/tasks/", wantErr: true},
		{name: "missing database name", dbURL: "postgres://u:p@localhost:5432", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHost, gotDBName, gotErr := parseDBURL(tt.dbURL)

			if tt.wantErr {
				if gotErr == nil {
					t.Fatalf("parseDBURL(%q) error = nil, want error %q", tt.dbURL, errDatabaseURL)
				}

				if !errors.Is(gotErr, errDatabaseURL) {
					t.Fatalf("parseDBURL(%q) unexpected error: %v", tt.dbURL, gotErr)
				}
				return
			}

			if tt.wantHost != gotHost {
				t.Errorf("parseDBURL(%q) host = %q, want %q", tt.dbURL, gotHost, tt.wantHost)
			}

			if tt.wantDBName != gotDBName {
				t.Errorf("parseDBURL(%q) dbName = %q, want %q", tt.dbURL, gotDBName, tt.wantDBName)
			}
		})
	}
}

func TestParseAudiences(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "empty value", raw: "", want: nil},
		{name: "only separators and spaces", raw: " , ,, ", want: nil},
		{name: "single audience", raw: "taskapi", want: []string{"taskapi"}},
		{name: "URL audience", raw: "https://api.example.com", want: []string{"https://api.example.com"}},
		{name: "multiple audiences keep their order", raw: "b,a,c", want: []string{"b", "a", "c"}},
		{name: "spaces and empty entries are ignored", raw: " a , ,b,", want: []string{"a", "b"}},
		{name: "duplicates are dropped", raw: "a,b,a", want: []string{"a", "b"}},
		{name: "case is kept", raw: "Api,api", want: []string{"Api", "api"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseAudiences(tt.raw)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("parseAudiences(%q) mismatch (-want +got):\n%s", tt.raw, diff)
			}
		})
	}
}

func TestDefaultArgon2Concurrency(t *testing.T) {
	tests := []struct {
		name          string
		cpus, threads int
		want          int
	}{
		{name: "cores divided by threads", cpus: 8, threads: 4, want: 2},
		{name: "rounds down", cpus: 10, threads: 4, want: 2},
		{name: "fewer cores than threads", cpus: 2, threads: 4, want: 1},
		{name: "single core", cpus: 1, threads: 1, want: 1},
		{name: "one thread per hash", cpus: 8, threads: 1, want: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultArgon2Concurrency(tt.cpus, tt.threads); got != tt.want {
				t.Errorf("defaultArgon2Concurrency(%d, %d) = %d, want %d", tt.cpus, tt.threads, got, tt.want)
			}
		})
	}
}

func TestLoadInviteOwner(t *testing.T) {
	checkLoad(t, loadInviteOwner, nil, []loadCase[inviteOwnerCfg]{
		{name: "defaults", want: inviteOwnerCfg{hourlyMax: 30}},
		{name: "valid INVITE_OWNER_HOURLY_MAX", env: map[string]string{"INVITE_OWNER_HOURLY_MAX": "100"}, want: inviteOwnerCfg{hourlyMax: 100}},
		{name: "zero INVITE_OWNER_HOURLY_MAX", env: map[string]string{"INVITE_OWNER_HOURLY_MAX": "0"}, wantErr: []string{"INVITE_OWNER_HOURLY_MAX", "0"}},
		{name: "non-numerical INVITE_OWNER_HOURLY_MAX", env: map[string]string{"INVITE_OWNER_HOURLY_MAX": "many"}, wantErr: []string{"INVITE_OWNER_HOURLY_MAX", "many"}},
	})
}
