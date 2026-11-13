package postgres

import (
	"os"
	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/email/emailtest"
)

func TestOutboxStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	emailtest.TestOutboxStore(t, func(t *testing.T) (email.OutboxStore, auth.UserStore) {
		t.Helper()
		q := newAuthQueries(t, url)
		return NewOutboxStore(q), NewUserStore(q)
	})
}
