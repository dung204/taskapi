package postgres

import (
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/testutil"
)

func TestSessionStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	authtest.TestSessionStore(t, func(t *testing.T) (auth.SessionStore, auth.UserStore) {
		t.Helper()
		q := newAuthQueries(t, url)
		return NewSessionStore(q), NewUserStore(q)
	})
}
