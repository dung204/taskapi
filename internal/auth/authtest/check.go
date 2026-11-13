package authtest

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/testutil"
)

// CheckUser fails the test if got differs from want, or if any of its
// timestamps is not in UTC. Pass go-cmp options to skip fields.
func CheckUser(tb testing.TB, got, want auth.User, opts ...cmp.Option) {
	tb.Helper()

	testutil.CheckUTC(tb, "created_at", got.CreatedAt)
	testutil.CheckUTC(tb, "updated_at", got.UpdatedAt)
	testutil.CheckUTCPtr(tb, "email_verified_at", got.EmailVerifiedAt)
	testutil.CheckUTCPtr(tb, "disabled_at", got.DisabledAt)
	testutil.CheckUTCPtr(tb, "tokens_valid_after", got.TokensValidAfter)

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("user mismatch (-want +got):\n%s", diff)
	}
}

// CheckSession fails the test if got differs from want, or if any of its
// timestamps is not in UTC. Pass go-cmp options to skip fields.
func CheckSession(tb testing.TB, got, want auth.Session, opts ...cmp.Option) {
	tb.Helper()

	testutil.CheckUTC(tb, "created_at", got.CreatedAt)
	testutil.CheckUTC(tb, "expires_at", got.ExpiresAt)
	testutil.CheckUTCPtr(tb, "rotated_at", got.RotatedAt)

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("session mismatch (-want +got):\n%s", diff)
	}
}
