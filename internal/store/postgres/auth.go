package postgres

import (
	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
)

func NewAuthStores(q *sqlcgen.Queries) auth.Stores {
	return auth.Stores{
		Users:      NewUserStore(q),
		UserTokens: NewUserTokenStore(q),
		Sessions:   NewSessionStore(q),
		Outbox:     NewOutboxStore(q),
	}
}
