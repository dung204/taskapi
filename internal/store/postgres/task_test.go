package postgres

import (
	"os"

	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

func TestTaskStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.TestStore(t, func(t *testing.T) task.Store {
		t.Helper()
		return newPGStore(t, url, "tasks")
	})
}

func BenchmarkTaskStore(b *testing.B) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		b.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.BenchmarkStore(b, func(tb testing.TB) task.Store {
		tb.Helper()
		return newPGStore(tb, url, "tasks")
	})
}
