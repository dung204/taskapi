package memory

import (
	"testing"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

func TestTaskStore(t *testing.T) {
	tasktest.TestStore(t, func(t *testing.T) task.Store {
		t.Helper()
		return NewTaskStore()
	})
}

func BenchmarkTaskStore(b *testing.B) {
	tasktest.BenchmarkStore(b, func(tb testing.TB) task.Store {
		tb.Helper()
		return NewTaskStore()
	})
}
