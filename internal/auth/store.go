package auth

import (
	"context"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
)

type SessionStore interface {
	Create(ctx context.Context, ss Session) error
	GetByRefreshTokenHash(ctx context.Context, hash []byte) (Session, error)
	Rotate(ctx context.Context, id uuid.UUID, oldHash, newHash []byte, now time.Time) error
	DeleteByID(ctx context.Context, id uuid.UUID) error
	DeleteByUserID(ctx context.Context, userID uuid.UUID) error
	DeleteExpired(ctx context.Context, now time.Time, limit int32) (int64, error)
}

type UserStore interface {
	Create(ctx context.Context, u User) error
	GetByID(ctx context.Context, id uuid.UUID) (User, error)
	GetByEmail(ctx context.Context, email string) (User, error)
	SetEmailVerified(ctx context.Context, id uuid.UUID, at time.Time) error
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string, at time.Time) error
	SetTokensValidAfter(ctx context.Context, id uuid.UUID, after time.Time) error
}

type UserTokenStore interface {
	Create(ctx context.Context, t UserToken) error
	Consume(ctx context.Context, hash []byte, purpose TokenPurpose, now time.Time) (userID uuid.UUID, err error)
	DeleteByUserAndPurpose(ctx context.Context, userID uuid.UUID, purpose TokenPurpose) error
	DeleteExpired(ctx context.Context, now time.Time, limit int32) (int64, error)
}

type OutboxWriter interface {
	Create(ctx context.Context, item email.OutboxItem) error
}

type Stores struct {
	Users      UserStore
	UserTokens UserTokenStore
	Sessions   SessionStore
	Outbox     OutboxWriter
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Stores) error) error
}
