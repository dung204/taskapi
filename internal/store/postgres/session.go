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

type SessionStore struct {
	q *sqlcgen.Queries
}

var _ auth.SessionStore = (*SessionStore)(nil)

func NewSessionStore(q *sqlcgen.Queries) *SessionStore {
	return &SessionStore{q: q}
}

// Create implements [auth.SessionStore].
func (s *SessionStore) Create(ctx context.Context, ss auth.Session) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "session", ss.ID.String(), err)
	}

	if err := s.q.CreateSession(ctx, sqlcgen.CreateSessionParams(ss)); err != nil {
		return formatError(err)
	}

	return nil
}

// DeleteByID implements [auth.SessionStore].
func (s *SessionStore) DeleteByID(ctx context.Context, id uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("delete", "session", id.String(), err)
	}

	if _, err := s.q.DeleteSessionByID(ctx, id); err != nil {
		return formatError(err)
	}

	return nil
}

// DeleteByUserID implements [auth.SessionStore].
func (s *SessionStore) DeleteByUserID(ctx context.Context, userID uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("delete", "sessions of user", userID.String(), err)
	}

	if _, err := s.q.DeleteSessionsByUserID(ctx, userID); err != nil {
		return formatError(err)
	}

	return nil
}

// DeleteExpired implements [auth.SessionStore].
func (s *SessionStore) DeleteExpired(ctx context.Context, now time.Time, limit int32) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("delete", "expired sessions", "", err)
	}

	affected, err := s.q.DeleteExpiredSessions(ctx, sqlcgen.DeleteExpiredSessionsParams{
		Now: now, Limit: limit,
	})
	if err != nil {
		return 0, formatError(err)
	}

	return affected, nil
}

// GetByRefreshTokenHash implements [auth.SessionStore].
func (s *SessionStore) GetByRefreshTokenHash(ctx context.Context, hash []byte) (auth.Session, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "session", "", err)
	}

	row, err := s.q.GetSessionByRefreshTokenHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.Session{}, auth.ErrSessionNotFound
	}

	if err != nil {
		return auth.Session{}, formatError(err)
	}

	return sessionFromRow(row), nil
}

// Rotate implements [auth.SessionStore].
func (s *SessionStore) Rotate(ctx context.Context, id uuid.UUID, oldHash, newHash []byte, now time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("rotate", "session", id.String(), err)
	}

	affected, err := s.q.RotateSession(ctx, sqlcgen.RotateSessionParams{
		NewHash: newHash, Now: &now, ID: id, OldHash: oldHash,
	})
	if err != nil {
		return formatError(err)
	}

	if affected == 0 {
		return auth.ErrSessionNotRotated
	}

	return nil
}

func sessionFromRow(r sqlcgen.Session) auth.Session {
	s := auth.Session(r)
	s.ExpiresAt = utc(s.ExpiresAt)
	s.CreatedAt = utc(s.CreatedAt)
	s.RotatedAt = utcPtr(s.RotatedAt)

	return s
}
