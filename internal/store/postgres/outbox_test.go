package postgres

import (
	"testing"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/email/emailtest"
	"github.com/dung204/taskapi/internal/testutil"
)

func TestOutboxStore(t *testing.T) {
	url := testutil.DatabaseURL(t)
	emailtest.TestOutboxStore(t, func(t *testing.T) (email.OutboxStore, auth.UserStore) {
		t.Helper()
		q := newAuthQueries(t, url)
		return NewOutboxStore(q), NewUserStore(q)
	})
}
