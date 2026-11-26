package postgres

import (
	"testing"

	"github.com/dung204/taskapi/internal/testutil"
)

func TestInvitationStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	runInvitationStoreSuite(t, func(t *testing.T) ShareDeps {
		t.Helper()
		return newShareDeps(t, url)
	})
}

func TestMemberStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	runMemberStoreSuite(t, func(t *testing.T) ShareDeps {
		t.Helper()
		return newShareDeps(t, url)
	})
}
