package tasktest

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
)

func CheckTask(tb testing.TB, got, want task.Task, opts ...cmp.Option) {
	tb.Helper()

	testutil.CheckUTC(tb, "created_at", got.CreatedAt)
	testutil.CheckUTC(tb, "updated_at", got.UpdatedAt)
	testutil.CheckUTCPtr(tb, "due_at", got.DueAt)

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
