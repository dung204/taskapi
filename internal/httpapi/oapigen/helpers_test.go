package oapigen

import (
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/task"
)

func sampleTask() task.Task {
	due := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	return task.Task{
		ID:          uuid.NewV7(),
		Title:       "write the benchmark",
		Description: "a description of realistic length",
		Status:      task.StatusTodo,
		DueAt:       &due,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
}

func BenchmarkToListFilter(b *testing.B) {
	limit, offset := 50, 10
	p := ListTasksParams{Limit: &limit, Offset: &offset}

	for b.Loop() {
		_, _ = p.ToListFilter()
	}
}

func BenchmarkToAPITask(b *testing.B) {
	t := sampleTask()

	for b.Loop() {
		_ = ToAPITask(t)
	}
}

func BenchmarkToAPITasks(b *testing.B) {
	for _, n := range []int{0, 10, 100} {
		ts := make([]task.Task, n)
		for i := range ts {
			ts[i] = sampleTask()
		}

		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for b.Loop() {
				_ = ToAPITasks(ts)
			}
		})
	}
}
