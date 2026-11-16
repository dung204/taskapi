package memory

import (
	"testing"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

func TestTaskStore(t *testing.T) {
	tasktest.TestStore(t, func(t *testing.T) (task.Store, func() uuid.UUID) {
		t.Helper()
		return NewTaskStore(), uuid.NewV7
	})
}

func BenchmarkTaskStore(b *testing.B) {
	tasktest.BenchmarkStore(b, func(tb testing.TB) task.Store {
		tb.Helper()
		return NewTaskStore()
	})
}
