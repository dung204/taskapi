package postgres

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
)

type UserStore struct {
	q *sqlcgen.Queries
}

var _ auth.UserStore = (*UserStore)(nil)

func NewUserStore(q *sqlcgen.Queries) *UserStore {
	return &UserStore{q: q}
}

// Create implements [auth.UserStore].
func (s *UserStore) Create(ctx context.Context, u auth.User) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "user", "", err)
	}

	err := s.q.CreateUser(ctx, sqlcgen.CreateUserParams(u))
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
		return auth.ErrEmailTaken
	}

	if err != nil {
		return formatError(err)
	}

	return nil
}

// GetByEmail implements [auth.UserStore].
func (s *UserStore) GetByEmail(ctx context.Context, email string) (auth.User, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "user by email", email, err)
	}

	row, err := s.q.GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}

	if err != nil {
		return auth.User{}, formatError(err)
	}

	return userFromRow(row), nil
}

// GetByID implements [auth.UserStore].
func (s *UserStore) GetByID(ctx context.Context, id uuid.UUID) (auth.User, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "user by ID", id.String(), err)
	}

	row, err := s.q.GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.User{}, auth.ErrUserNotFound
	}

	if err != nil {
		return auth.User{}, formatError(err)
	}

	return userFromRow(row), nil
}

// SetEmailVerified implements [auth.UserStore].
func (s *UserStore) SetEmailVerified(ctx context.Context, id uuid.UUID, at time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("set", "user email verified", id.String(), err)
	}

	affected, err := s.q.SetUserEmailVerified(ctx, sqlcgen.SetUserEmailVerifiedParams{
		EmailVerifiedAt: &at, UpdatedAt: at, ID: id,
	})
	if err != nil {
		return formatError(err)
	}

	if affected == 0 {
		return auth.ErrUserNotFound
	}

	return nil
}

// UpdatePassword implements [auth.UserStore].
func (s *UserStore) UpdatePassword(ctx context.Context, id uuid.UUID, hash string, at time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("update", "user password", id.String(), err)
	}

	affected, err := s.q.UpdateUserPassword(ctx, sqlcgen.UpdateUserPasswordParams{
		PasswordHash: &hash, At: at, ID: id,
	})
	if err != nil {
		return formatError(err)
	}

	if affected == 0 {
		return auth.ErrUserNotFound
	}

	return nil
}

// SetTokensValidAfter implements [auth.UserStore].
func (s *UserStore) SetTokensValidAfter(ctx context.Context, id uuid.UUID, after time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("set", "tokens valid after for user", id.String(), err)
	}

	affected, err := s.q.SetTokensValidAfterForUser(ctx, sqlcgen.SetTokensValidAfterForUserParams{
		At: after, ID: id,
	})
	if err != nil {
		return formatError(err)
	}

	if affected == 0 {
		return auth.ErrUserNotFound
	}

	return nil
}

func userFromRow(r sqlcgen.User) auth.User {
	s := auth.User(r)
	s.CreatedAt = s.CreatedAt.UTC()
	s.UpdatedAt = s.UpdatedAt.UTC()
	if s.EmailVerifiedAt != nil {
		s.EmailVerifiedAt = new(s.EmailVerifiedAt.UTC())
	}
	if s.DisabledAt != nil {
		s.DisabledAt = new(s.DisabledAt.UTC())
	}
	if s.TokensValidAfter != nil {
		s.TokensValidAfter = new(s.TokensValidAfter.UTC())
	}

	return s
}
