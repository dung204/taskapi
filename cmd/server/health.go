package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type check struct {
	name     string
	ping     func(ctx context.Context) error
	critical bool
	healthy  atomic.Bool
}

func newCheck(name string, critical bool, ping func(ctx context.Context) error) *check {
	c := &check{name: name, ping: ping, critical: critical}
	c.healthy.Store(true) // a failure on the first probe is a transition worth logging
	return c
}

func (c *check) run(ctx context.Context, l *slog.Logger) error {
	err := c.ping(ctx)
	ok := err == nil
	if c.healthy.Swap(ok) != ok {
		if ok {
			l.InfoContext(ctx, "health check recovered", "check", c.name)
		} else {
			l.WarnContext(ctx, "health check degraded", "check", c.name)
		}
	}
	return err
}

const (
	HealthStatusOK          = "ok"
	HealthStatusDegraded    = "degraded"
	HealthStatusUnavailable = "unavailable"
)

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func runChecks(ctx context.Context, l *slog.Logger, checks []*check) []error {
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	errs := make([]error, len(checks))
	var wg sync.WaitGroup
	for i := range checks {
		wg.Go(func() { errs[i] = checks[i].run(ctx, l) })
	}
	wg.Wait()
	return errs
}

func summarize(checks []*check, errs []error) (healthResponse, int) {
	status, code := HealthStatusOK, http.StatusOK
	results := make(map[string]string, len(checks))
	for i, c := range checks {
		if errs[i] == nil {
			results[c.name] = HealthStatusOK
			continue
		}

		results[c.name] = HealthStatusUnavailable
		switch {
		case c.critical:
			status, code = HealthStatusUnavailable, http.StatusServiceUnavailable
		case status == HealthStatusOK:
			status = HealthStatusDegraded
		}
	}

	return healthResponse{Status: status, Checks: results}, code
}

func writeJSON(w http.ResponseWriter, code int, resp healthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(resp)
}

func healthHandler(l *slog.Logger, checks ...*check) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errs := runChecks(r.Context(), l, checks)
		resp, code := summarize(checks, errs)
		writeJSON(w, code, resp)
	})
}

func healthURL(addr string) (string, error) {
	if addr == "" {
		return "", errors.New("OPS_ADDR is not set")
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}

	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

func runHealthcheck(lookup lookupFunc) int {
	url, err := healthURL(getenv(lookup, "OPS_ADDR", ""))
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: status %d\n", resp.StatusCode)
		return 1
	}

	return 0
}
