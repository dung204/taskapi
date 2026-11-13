package postgres

import (
	"os"
	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
)

func TestUserTokenStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	authtest.TestUserTokenStore(t, func(t *testing.T) (auth.UserTokenStore, auth.UserStore) {
		t.Helper()
		q := newAuthQueries(t, url)
		return NewUserTokenStore(q), NewUserStore(q)
	})
}
