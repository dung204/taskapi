package task_test

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"uuid"
)

var errBackend = errors.New("redis is down")

func upper(s string) string { return strings.ToUpper(s) }

func uuidString(i int) string { return fmt.Sprintf("%04d-%s", i, uuid.NewV7()) }

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

// wantErr fails unless err wraps want.
func wantErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
