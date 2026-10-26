package tasktest

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/dung204/taskapi/internal/task"
)

func checkUTC(tb testing.TB, field string, got time.Time) (hasError bool) {
	tb.Helper()

	if hasError = got.Location() != time.UTC; hasError {
		tb.Errorf("%s location = %v, want UTC", field, got.Location())
	}
	return hasError
}

func CheckTask(tb testing.TB, got, want task.Task, opts ...cmp.Option) {
	tb.Helper()

	checkUTC(tb, "created_at", got.CreatedAt)
	checkUTC(tb, "updated_at", got.UpdatedAt)
	if got.DueAt != nil {
		checkUTC(tb, "due_at", *got.DueAt)
	}

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("task mismatch (-want +got):\n%s", diff)
	}
}

func CheckPatch(tb testing.TB, got, want task.Patch, opts ...cmp.Option) (hasError bool) {
	tb.Helper()
	hasError = false

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("patch mismatch (-want +got):\n%s", diff)
		hasError = true
	}

	return hasError
}
