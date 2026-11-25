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

// CheckInvitation compares two invitations and checks that got holds UTC times.
func CheckInvitation(tb testing.TB, got, want task.Invitation, opts ...cmp.Option) {
	tb.Helper()

	testutil.CheckUTC(tb, "expires_at", got.ExpiresAt)
	testutil.CheckUTC(tb, "created_at", got.CreatedAt)
	testutil.CheckUTC(tb, "updated_at", got.UpdatedAt)

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("invitation mismatch (-want +got):\n%s", diff)
	}
}

// CheckMember compares two members and checks that got holds UTC times.
func CheckMember(tb testing.TB, got, want task.Member, opts ...cmp.Option) {
	tb.Helper()

	testutil.CheckUTC(tb, "created_at", got.CreatedAt)
	testutil.CheckUTC(tb, "updated_at", got.UpdatedAt)

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("member mismatch (-want +got):\n%s", diff)
	}
}

// CheckShare compares two shares and checks that got holds UTC times.
func CheckShare(tb testing.TB, got, want task.Share, opts ...cmp.Option) {
	tb.Helper()

	testutil.CheckUTC(tb, "created_at", got.CreatedAt)
	testutil.CheckUTCPtr(tb, "expires_at", got.ExpiresAt)

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("share mismatch (-want +got):\n%s", diff)
	}
}
