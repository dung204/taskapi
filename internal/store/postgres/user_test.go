package postgres

import (
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/testutil"
)

func TestUserStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	authtest.TestUserStore(t, func(t *testing.T) auth.UserStore {
		t.Helper()
		return NewUserStore(newAuthQueries(t, url))
	})
}
