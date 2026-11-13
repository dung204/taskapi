package postgres

import (
	"os"
	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
)

func TestSessionStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	authtest.TestSessionStore(t, func(t *testing.T) (auth.SessionStore, auth.UserStore) {
		t.Helper()
		q := newAuthQueries(t, url)
		return NewSessionStore(q), NewUserStore(q)
	})
}
