package postgres

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
)

type UserTokenStore struct {
	q *sqlcgen.Queries
}

var _ auth.UserTokenStore = (*UserTokenStore)(nil)

func NewUserTokenStore(q *sqlcgen.Queries) *UserTokenStore {
	return &UserTokenStore{q: q}
}

// Consume implements [auth.UserTokenStore].
func (s *UserTokenStore) Consume(ctx context.Context, hash []byte, purpose auth.TokenPurpose, now time.Time) (uuid.UUID, error) {
	formatError := func(err error) error {
		return formatPostgresError("consume", "user token", "", err)
	}

	userID, err := s.q.ConsumeUserToken(ctx, sqlcgen.ConsumeUserTokenParams{
		Now: &now, TokenHash: hash, Purpose: purpose,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil(), auth.ErrUserTokenInvalid
	}

	if err != nil {
		return uuid.Nil(), formatError(err)
	}

	return userID, nil
}

// Create implements [auth.UserTokenStore].
func (s *UserTokenStore) Create(ctx context.Context, t auth.UserToken) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "user token", "", err)
	}

	if err := s.q.CreateUserToken(ctx, sqlcgen.CreateUserTokenParams(t)); err != nil {
		return formatError(err)
	}

	return nil
}

// DeleteByUserAndPurpose implements [auth.UserTokenStore].
func (s *UserTokenStore) DeleteByUserAndPurpose(ctx context.Context, userID uuid.UUID, purpose auth.TokenPurpose) error {
	formatError := func(err error) error {
		return formatPostgresError("delete", "user tokens by user and purpose", "", err)
	}

	if err := s.q.DeleteUserTokensByUserAndPurpose(ctx, sqlcgen.DeleteUserTokensByUserAndPurposeParams{UserID: userID, Purpose: purpose}); err != nil {
		return formatError(err)
	}

	return nil
}

// DeleteExpired implements [auth.UserTokenStore].
func (s *UserTokenStore) DeleteExpired(ctx context.Context, now time.Time, limit int32) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("delete", "expired user tokens", "", err)
	}

	affected, err := s.q.DeleteExpiredUserTokens(ctx, sqlcgen.DeleteExpiredUserTokensParams{
		Now: now, Limit: limit,
	})
	if err != nil {
		return 0, formatError(err)
	}

	return affected, nil
}
