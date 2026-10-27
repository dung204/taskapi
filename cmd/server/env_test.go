package main

import (
	"errors"
	"log/slog"
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
		{name: "valid METRICS_ADDR", env: map[string]string{"METRICS_ADDR": "localhost:9090"}, want: cfgWith(func(c *config) { c.metricsAddr = "localhost:9090" })},

		{name: "invalid PORT", env: map[string]string{"PORT": "abc"}, wantErr: []string{"PORT", "abc"}},
		{name: "invalid STORE", env: map[string]string{"STORE": "foo"}, wantErr: []string{"STORE", "foo"}},
		{name: "invalid LOG_LEVEL", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: []string{"LOG_LEVEL", "loud"}},
		{name: "invalid LOG_FORMAT", env: map[string]string{"LOG_FORMAT": "yaml"}, wantErr: []string{"LOG_FORMAT", "yaml"}},
		{name: "invalid WORKER_ENABLED", env: map[string]string{"WORKER_ENABLED": "maybe"}, wantErr: []string{"WORKER_ENABLED", "maybe"}},
		{name: "invalid PPROF_ADDR", env: map[string]string{"PPROF_ADDR": "6060"}, wantErr: []string{"PPROF_ADDR", "6060"}},
		{name: "invalid METRICS_ADDR", env: map[string]string{"METRICS_ADDR": "9090"}, wantErr: []string{"METRICS_ADDR", "9090"}},
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
		{name: "empty value", value: ""},
		{name: "port only", value: ":6060"},
		{name: "hostname and port", value: "localhost:6060"},
		{name: "IPv4 and port", value: "127.0.0.1:6060"},
		{name: "IPv6 and port", value: "[::1]:6060"},
		{name: "max port", value: "0.0.0.0:65535"},

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
