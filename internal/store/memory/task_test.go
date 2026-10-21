package memory

import (
	"testing"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/task/tasktest"
)

func TestTaskStore(t *testing.T) {
	tasktest.TestStore(t, func(t *testing.T) task.Store {
		return NewTaskStore()
	})
}
