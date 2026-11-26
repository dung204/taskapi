package postgres

import (
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/testutil"
)

func TestUserTokenStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	authtest.TestUserTokenStore(t, func(t *testing.T) (auth.UserTokenStore, auth.UserStore) {
		t.Helper()
		q := newAuthQueries(t, url)
		return NewUserTokenStore(q), NewUserStore(q)
	})
}
