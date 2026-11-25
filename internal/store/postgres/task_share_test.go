package postgres

import (
	"os"
	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/task/tasktest"
)

func TestInvitationStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.TestInvitationStore(t, func(t *testing.T) tasktest.ShareDeps {
		t.Helper()
		return newShareDeps(t, url)
	})
}

func TestMemberStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	tasktest.TestMemberStore(t, func(t *testing.T) tasktest.ShareDeps {
		t.Helper()
		return newShareDeps(t, url)
	})
}
