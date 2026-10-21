package main

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func defaultConfig() config {
	return config{
		port:           8080,
		store:          "memory",
		logFormat:      "text",
		logLevel:       slog.LevelInfo,
		workerEnabled:  true,
		workerInterval: 30 * time.Second,
	}
}

func cfgWith(mod func(*config)) config {
	c := defaultConfig()
	mod(&c)
	return c
}

func checkConfig(t *testing.T, got, want config) {
	t.Helper()
	if got.port != want.port {
		t.Errorf("port = %v, want %v", got.port, want.port)
	}
	if got.store != want.store {
		t.Errorf("store = %q, want %q", got.store, want.store)
	}
	if got.logFormat != want.logFormat {
		t.Errorf("logFormat = %q, want %q", got.logFormat, want.logFormat)
	}
	if got.logLevel != want.logLevel {
		t.Errorf("logLevel = %q, want %q", got.logLevel.String(), want.logLevel.String())
	}
	if got.workerEnabled != want.workerEnabled {
		t.Errorf("workerEnabled = %t, want %t", got.workerEnabled, want.workerEnabled)
	}
	if got.workerInterval != want.workerInterval {
		t.Errorf("workerInterval = %v, want %v", got.workerInterval, want.workerInterval)
	}
	if got.dbURL != want.dbURL {
		t.Errorf("dbURL = %v, want %v", got.dbURL, want.dbURL)
	}
	if got.dbHost != want.dbHost {
		t.Errorf("dbHost = %v, want %v", got.dbHost, want.dbHost)
	}
	if got.dbName != want.dbName {
		t.Errorf("dbName = %v, want %v", got.dbName, want.dbName)
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

		{name: "valid PORT", env: map[string]string{"PORT": "9000"}, want: cfgWith(func(c *config) { c.port = 9000 })},
		{name: "valid STORE", env: map[string]string{"STORE": "memory"}, want: cfgWith(func(c *config) { c.store = "memory" })},
		{name: "valid LOG_FORMAT", env: map[string]string{"LOG_FORMAT": "json"}, want: cfgWith(func(c *config) { c.logFormat = "json" })},
		{name: "valid LOG_LEVEL", env: map[string]string{"LOG_LEVEL": "debug"}, want: cfgWith(func(c *config) { c.logLevel = slog.LevelDebug })},
		{name: "valid WORKER_ENABLED", env: map[string]string{"WORKER_ENABLED": "false"}, want: cfgWith(func(c *config) { c.workerEnabled = false })},

		{name: "invalid PORT", env: map[string]string{"PORT": "abc"}, wantErr: []string{"PORT", "abc"}},
		{name: "invalid STORE", env: map[string]string{"STORE": "foo"}, wantErr: []string{"STORE", "foo"}},
		{name: "invalid LOG_LEVEL", env: map[string]string{"LOG_LEVEL": "loud"}, wantErr: []string{"LOG_LEVEL", "loud"}},
		{name: "invalid LOG_FORMAT", env: map[string]string{"LOG_FORMAT": "yaml"}, wantErr: []string{"LOG_FORMAT", "yaml"}},
		{name: "invalid WORKER_ENABLED", env: map[string]string{"WORKER_ENABLED": "maybe"}, wantErr: []string{"WORKER_ENABLED", "maybe"}},

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

func TestParseDBURL(t *testing.T) {
	tests := []struct {
		name       string
		dbURL      string
		wantHost   string
		wantDBName string
		wantErr    bool
	}{
		{name: "valid URL", dbURL: "postgres://u:p@localhost:5432/tasks", wantHost: "localhost", wantDBName: "tasks"},
		{name: "unparseable URL", dbURL: "postgres://u:p@localhost:abc/tasks", wantErr: true},
		{name: "wrong scheme", dbURL: "https://u:p@localhost:5432/tasks", wantErr: true},
		{name: "missing host", dbURL: "postgres://u:p@:5432/tasks", wantErr: true},
		{name: "missing port", dbURL: "postgres://u:p@localhost/tasks", wantHost: "localhost", wantDBName: "tasks"},
		{name: "port out of range", dbURL: "postgres://u:p@localhost:65536/tasks", wantErr: true},
		{name: "database name contains slash", dbURL: "postgres://u:p@localhost:5432/tasks/prod", wantErr: true},
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
