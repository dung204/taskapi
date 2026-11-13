package emailtest

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dung204/taskapi/internal/email"
)

// CheckClaimed fails the test if got differs from want. Claim does not promise
// an order for the items it returns, so both sides are compared sorted by ID.
// Pass go-cmp options to skip fields.
func CheckClaimed(tb testing.TB, got, want []email.ClaimedItem, opts ...cmp.Option) {
	tb.Helper()

	opts = append([]cmp.Option{
		cmpopts.EquateEmpty(),
		cmpopts.SortSlices(func(a, b email.ClaimedItem) bool { return a.ID.Compare(b.ID) < 0 }),
	}, opts...)

	if diff := cmp.Diff(want, got, opts...); diff != "" {
		tb.Errorf("claimed items mismatch (-want +got):\n%s", diff)
	}
}
