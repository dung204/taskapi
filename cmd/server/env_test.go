package main

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func defaultConfig() config {
	return config{
		port:           8080,
		store:          "memory",
		logFormat:      "text",
		logLevel:       slog.LevelInfo,
		workerEnabled:  true,
		workerInterval: 30 * time.Second,
		pprofAddr:      "",
		opsAddr:        "127.0.0.1:9091",
		rateLimitRPS:   10,
		rateLimitBurst: 20,
		corsOrigins:    nil,
		corsMaxAge:     10 * time.Minute,
	}
}

func cfgWith(mod func(*config)) config {
	c := defaultConfig()
	mod(&c)
	return c
}

func checkConfig(t *testing.T, got, want config, opts ...cmp.Option) {
	t.Helper()

	opts = append(opts, cmp.AllowUnexported(config{}))
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		t.Errorf("env mismatch (-want +got):\n%s", diff)
	}
}

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config
		wantErr []string
	}{
		{name: "defaults", want: defaultConfig()},
		{name: "field with empty value", env: map[string]string{"PORT": "", "LOG_FORMAT": "json"}, want: cfgWith(func(c *config) { c.logFormat = "json" })},

		{name: "valid PORT", env: map[string]string{"PORT": "9000"}, want: cfgWith(func(c *config) { c.port = 9000 })},
		{name: "valid STORE", env: map[string]string{"STORE": "memory"}, want: cfgWith(func(c *config) { c.store = "memory" })},
		{name: "valid LOG_FORMAT", env: map[string]string{"LOG_FORMAT": "json"}, want: cfgWith(func(c *config) { c.logFormat = "json" })},
		{name: "valid LOG_LEVEL", env: map[string]string{"LOG_LEVEL": "debug"}, want: cfgWith(func(c *config) { c.logLevel = slog.LevelDebug })},
		{name: "valid WORKER_ENABLED", env: map[string]string{"WORKER_ENABLED": "false"}, want: cfgWith(func(c *config) { c.workerEnabled = false })},
		{name: "valid PPROF_ADDR", env: map[string]string{"PPROF_ADDR": ":6060"}, want: cfgWith(func(c *config) { c.pprofAddr = ":6060" })},
		{name: "empty PPROF_ADDR", env: map[string]string{"PPROF_ADDR": ""}, want: cfgWith(func(c *config) { c.pprofAddr = "" })},
		{name: "valid OPS_ADDR", env: map[string]string{"OPS_ADDR": "localhost:9090"}, want: cfgWith(func(c *config) { c.opsAddr = "localhost:9090" })},
		{name: "valid RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "0.5"}, want: cfgWith(func(c *config) { c.rateLimitRPS = 0.5 })},
		{name: "valid RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "5"}, want: cfgWith(func(c *config) { c.rateLimitBurst = 5 })},
		{name: "valid CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"https://app.example.com"} })},
		{name: "multiple CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com, http://localhost:5173"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"https://app.example.com", "http://localhost:5173"} })},
		{name: "CORS_ALLOWED_ORIGINS ignores empty entries", env: map[string]string{"CORS_ALLOWED_ORIGINS": ",https://app.example.com,,"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"https://app.example.com"} })},
		{name: "CORS_ALLOWED_ORIGINS is lowercased", env: map[string]string{"CORS_ALLOWED_ORIGINS": "HTTPS://App.Example.COM:8443"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"https://app.example.com:8443"} })},
		{name: "CORS_ALLOWED_ORIGINS drops default ports", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com:443,http://localhost:80"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"https://app.example.com", "http://localhost"} })},
		{name: "CORS_ALLOWED_ORIGINS keeps non-default ports", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com:80,http://localhost:443"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"https://app.example.com:80", "http://localhost:443"} })},
		{name: "CORS_ALLOWED_ORIGINS drops duplicates", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com,HTTPS://APP.example.com:443"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"https://app.example.com"} })},
		{name: "wildcard CORS_ALLOWED_ORIGINS", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"*"} })},
		{name: "valid CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "1h"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"*"}; c.corsMaxAge = time.Hour })},
		{name: "zero CORS_MAX_AGE omits the header", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "0"}, want: cfgWith(func(c *config) { c.corsOrigins = []string{"*"}; c.corsMaxAge = 0 })},

		{name: "invalid PORT", env: map[string]string{"PORT": "abc"}, wantErr: []string{"PORT", "abc"}},
		{name: "invalid STORE", env: map[string]string{"STORE": "foo"}, wantErr: []string{"STORE", "foo"}},
		{name: "invalid LOG_LEVEL", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: []string{"LOG_LEVEL", "loud"}},
		{name: "invalid LOG_FORMAT", env: map[string]string{"LOG_FORMAT": "yaml"}, wantErr: []string{"LOG_FORMAT", "yaml"}},
		{name: "invalid WORKER_ENABLED", env: map[string]string{"WORKER_ENABLED": "maybe"}, wantErr: []string{"WORKER_ENABLED", "maybe"}},
		{name: "invalid PPROF_ADDR", env: map[string]string{"PPROF_ADDR": "6060"}, wantErr: []string{"PPROF_ADDR", "6060"}},
		{name: "invalid OPS_ADDR", env: map[string]string{"OPS_ADDR": "9090"}, wantErr: []string{"OPS_ADDR", "9090"}},
		{name: "non-numerical RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "abc"}, wantErr: []string{"RATE_LIMIT_RPS", "abc"}},
		{name: "negative RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "-1"}, wantErr: []string{"RATE_LIMIT_RPS", "-1"}},
		{name: "NaN RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "NaN"}, wantErr: []string{"RATE_LIMIT_RPS", "NaN"}},
		{name: "Inf RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "Inf"}, wantErr: []string{"RATE_LIMIT_RPS", "Inf"}},
		{name: "-Inf RATE_LIMIT_RPS", env: map[string]string{"RATE_LIMIT_RPS": "-Inf"}, wantErr: []string{"RATE_LIMIT_RPS", "-Inf"}},
		{name: "non-numerical RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "abc"}, wantErr: []string{"RATE_LIMIT_BURST", "abc"}},
		{name: "fractional RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "1.5"}, wantErr: []string{"RATE_LIMIT_BURST", "1.5"}},
		{name: "zero RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "0"}, wantErr: []string{"RATE_LIMIT_BURST", "0"}},
		{name: "negative RATE_LIMIT_BURST", env: map[string]string{"RATE_LIMIT_BURST": "-5"}, wantErr: []string{"RATE_LIMIT_BURST", "-5"}},
		{name: "CORS_ALLOWED_ORIGINS without scheme", env: map[string]string{"CORS_ALLOWED_ORIGINS": "app.example.com"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "app.example.com"}},
		{name: "CORS_ALLOWED_ORIGINS with unsupported scheme", env: map[string]string{"CORS_ALLOWED_ORIGINS": "ftp://app.example.com"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "ftp://app.example.com"}},
		{name: "CORS_ALLOWED_ORIGINS with trailing slash", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com/"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com/"}},
		{name: "CORS_ALLOWED_ORIGINS with path", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com/app"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com/app"}},
		{name: "CORS_ALLOWED_ORIGINS with query", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com?a=b"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://app.example.com?a=b"}},
		{name: "CORS_ALLOWED_ORIGINS with credentials", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://u:p@app.example.com"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://u:p@app.example.com"}},
		{name: "CORS_ALLOWED_ORIGINS without host", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://"}},
		{name: "CORS_ALLOWED_ORIGINS with invalid port", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://app.example.com:99999"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "99999"}},
		{name: "CORS_ALLOWED_ORIGINS wildcard mixed with origins", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*,https://app.example.com"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "*"}},
		{name: "CORS_ALLOWED_ORIGINS wildcard subdomain", env: map[string]string{"CORS_ALLOWED_ORIGINS": "https://*.example.com"}, wantErr: []string{"CORS_ALLOWED_ORIGINS", "https://*.example.com"}},
		{name: "non-duration CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "abc"}, wantErr: []string{"CORS_MAX_AGE", "abc"}},
		{name: "negative CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "-1s"}, wantErr: []string{"CORS_MAX_AGE", "-1s"}},
		{name: "sub-second CORS_MAX_AGE", env: map[string]string{"CORS_ALLOWED_ORIGINS": "*", "CORS_MAX_AGE": "1500ms"}, wantErr: []string{"CORS_MAX_AGE", "1500ms"}},
		{name: "multiple errors", env: map[string]string{"PORT": "abc", "STORE": "foo", "LOG_LEVEL": "loud"}, wantErr: []string{"PORT", "abc", "STORE", "foo", "LOG_LEVEL", "loud"}},

		{
			name: "STORE=postgres with valid DATABASE_URL",
			env:  map[string]string{"STORE": "postgres", "DATABASE_URL": "postgres://u:p@host:5432/db"},
			want: cfgWith(func(c *config) {
				c.store = "postgres"
				c.dbURL = "postgres://u:p@host:5432/db"
				c.dbHost = "host"
				c.dbName = "db"
			}),
		},
		{name: "STORE=postgres with invalid DATABASE_URL", env: map[string]string{"STORE": "postgres", "DATABASE_URL": "abcd1234"}, wantErr: []string{"DATABASE_URL"}},
		{name: "STORE=postgres without DATABASE_URL", env: map[string]string{"STORE": "postgres"}, wantErr: []string{"DATABASE_URL"}},
		{
			name: "STORE=memory with any DATABASE_URL",
			env:  map[string]string{"STORE": "memory", "DATABASE_URL": "abc1234"},
			want: cfgWith(func(c *config) {
				c.store = "memory"
				c.dbURL = ""
			}),
		},

		{
			name: "WORKER_ENABLED=true with valid WORKER_INTERVAL",
			env:  map[string]string{"WORKER_ENABLED": "true", "WORKER_INTERVAL": "500ms"},
			want: cfgWith(func(c *config) {
				c.workerEnabled = true
				c.workerInterval = 500 * time.Millisecond
			}),
		},
		{
			name:    "WORKER_ENABLED=true with non-numerical WORKER_INTERVAL",
			env:     map[string]string{"WORKER_ENABLED": "true", "WORKER_INTERVAL": "abc"},
			wantErr: []string{"WORKER_INTERVAL", "abc"},
		},
		{
			name:    "WORKER_ENABLED=true with non-positive WORKER_INTERVAL",
			env:     map[string]string{"WORKER_ENABLED": "true", "WORKER_INTERVAL": "0s"},
			wantErr: []string{"WORKER_INTERVAL", "0s"},
		},
		{
			name: "WORKER_ENABLED=false with invalid WORKER_INTERVAL",
			env:  map[string]string{"WORKER_ENABLED": "false", "WORKER_INTERVAL": "abc"},
			want: cfgWith(func(c *config) {
				c.workerEnabled = false
			}),
		},

		{
			name: "RATE_LIMIT_RPS=0 disables rate limiting",
			env:  map[string]string{"RATE_LIMIT_RPS": "0"},
			want: cfgWith(func(c *config) {
				c.rateLimitRPS = 0
				c.rateLimitBurst = 0
			}),
		},
		{
			name: "RATE_LIMIT_RPS=0 with invalid RATE_LIMIT_BURST",
			env:  map[string]string{"RATE_LIMIT_RPS": "0", "RATE_LIMIT_BURST": "abc"},
			want: cfgWith(func(c *config) {
				c.rateLimitRPS = 0
				c.rateLimitBurst = 0
			}),
		},

		{
			name: "CORS_MAX_AGE is not checked without CORS_ALLOWED_ORIGINS",
			env:  map[string]string{"CORS_MAX_AGE": "abc"},
			want: defaultConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := loadConfig(func(k string) string { return tt.env[k] })
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("loadConfig() error = nil, want error containing %q", tt.wantErr)
				}
				for _, s := range tt.wantErr {
					if !strings.Contains(err.Error(), s) {
						t.Errorf("loadConfig() error = %q, want it to contain %q", err, s)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("loadConfig() unexpected error: %v", err)
			}

			checkConfig(t, got, tt.want)
		})
	}
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
