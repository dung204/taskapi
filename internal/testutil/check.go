package testutil

import (
	"testing"
	"time"
)

// CheckUTC fails the test if got is not in the UTC location.
func CheckUTC(tb testing.TB, field string, got time.Time) {
	tb.Helper()

	if got.Location() != time.UTC {
		tb.Errorf("%s location = %v, want UTC", field, got.Location())
	}
}

// CheckUTCPtr is like [CheckUTC] but skips a nil time.
func CheckUTCPtr(tb testing.TB, field string, got *time.Time) {
	tb.Helper()

	if got != nil {
		CheckUTC(tb, field, *got)
	}
}
