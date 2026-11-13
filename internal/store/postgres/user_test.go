package postgres

import (
	"os"
	"strings"
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
)

func TestUserStore(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	authtest.TestUserStore(t, func(t *testing.T) auth.UserStore {
		t.Helper()
		return NewUserStore(newAuthQueries(t, url))
	})
}
