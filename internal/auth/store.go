package auth

import (
	"context"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
)

// SessionStore persists login sessions, each identified by a refresh token
// that is stored only as a hash.
type SessionStore interface {
	// Create persists a [Session].
	Create(ctx context.Context, ss Session) error

	// GetByRefreshTokenHash fetches the [Session] whose current or previous
	// refresh token hash equals hash, so a client that presents the token it had
	// just before a rotation is still recognized. It does not check whether the
	// session has expired.
	//
	// Returns:
	//   - session - the matching [Session].
	//
	// Returns the following errors:
	//   - [ErrSessionNotFound] - if no session matches.
	GetByRefreshTokenHash(ctx context.Context, hash []byte) (session Session, err error)

	// Rotate replaces the refresh token hash oldHash of the session id with
	// newHash, keeps oldHash as the previous hash and records now as the rotation
	// time.
	//
	// Returns the following errors:
	//   - [ErrSessionNotRotated] - if the session does not exist, its current
	//     hash is not oldHash (it was already rotated), or it has expired at now.
	Rotate(ctx context.Context, id uuid.UUID, oldHash, newHash []byte, now time.Time) error

	// DeleteByID removes the session with the given id (logout). Removing a
	// session that does not exist is not an error.
	DeleteByID(ctx context.Context, id uuid.UUID) error

	// DeleteByUserID removes every session of the user (logout everywhere).
	// Having no session is not an error.
	DeleteByUserID(ctx context.Context, userID uuid.UUID) error

	// DeleteExpired removes up to limit sessions that expire at or before now.
	//
	// Returns:
	//   - deleted - the number of sessions removed.
	DeleteExpired(ctx context.Context, now time.Time, limit int32) (deleted int64, err error)
}

// UserStore persists user accounts.
type UserStore interface {
	// Create persists a [User].
	//
	// Returns the following errors:
	//   - [ErrEmailTaken] - if another user already has the same email.
	Create(ctx context.Context, u User) error

	// GetByID fetches the [User] with the given id.
	//
	// Returns:
	//   - user - the [User] with the given id.
	//
	// Returns the following errors:
	//   - [ErrUserNotFound] - if no user has the given id.
	GetByID(ctx context.Context, id uuid.UUID) (user User, err error)

	// GetByEmail fetches the [User] with the given email, which must already be
	// normalized (see [email.Parse]).
	//
	// Returns:
	//   - user - the [User] with the given email.
	//
	// Returns the following errors:
	//   - [ErrUserNotFound] - if no user has the given email.
	GetByEmail(ctx context.Context, email string) (user User, err error)

	// SetEmailVerified marks the user's email as verified at at.
	//
	// Returns the following errors:
	//   - [ErrUserNotFound] - if the user does not exist.
	SetEmailVerified(ctx context.Context, id uuid.UUID, at time.Time) error

	// UpdatePassword replaces the user's password hash with hash and, as a
	// security measure, makes access tokens issued before at invalid (see
	// SetTokensValidAfter).
	//
	// Returns the following errors:
	//   - [ErrUserNotFound] - if the user does not exist.
	UpdatePassword(ctx context.Context, id uuid.UUID, hash string, at time.Time) error

	// SetTokensValidAfter makes access tokens issued before after invalid. The
	// cutoff only moves forward: an earlier value than the current one is
	// ignored.
	//
	// Returns the following errors:
	//   - [ErrUserNotFound] - if the user does not exist.
	SetTokensValidAfter(ctx context.Context, id uuid.UUID, after time.Time) error
}

// UserTokenStore persists the one-time tokens sent to users by email (email
// verification, password reset), stored only as a hash.
type UserTokenStore interface {
	// Create persists a [UserToken].
	Create(ctx context.Context, t UserToken) error

	// Consume marks the token with the given hash and purpose as used at now, so
	// a token can be consumed only once.
	//
	// Returns:
	//   - userID - the id of the user the token belongs to.
	//
	// Returns the following errors:
	//   - [ErrUserTokenInvalid] - if there is no such token, it was already used,
	//     or it has expired at now.
	Consume(ctx context.Context, hash []byte, purpose TokenPurpose, now time.Time) (userID uuid.UUID, err error)

	// DeleteByUserAndPurpose removes every token of the user for the given
	// purpose, used or not, such as when a new one is issued. Having none is not
	// an error.
	DeleteByUserAndPurpose(ctx context.Context, userID uuid.UUID, purpose TokenPurpose) error

	// DeleteExpired removes up to limit tokens that expire at or before now.
	//
	// Returns:
	//   - deleted - the number of tokens removed.
	DeleteExpired(ctx context.Context, now time.Time, limit int32) (deleted int64, err error)
}

// OutboxWriter lets the auth service queue an email in the same transaction as
// the change that needs it.
type OutboxWriter interface {
	// Create persists an [email.OutboxItem] to be delivered later.
	Create(ctx context.Context, item email.OutboxItem) error
}

type Stores struct {
	Users      UserStore
	UserTokens UserTokenStore
	Sessions   SessionStore
	Outbox     OutboxWriter
}

// transactor runs several store operations as one unit.
type transactor interface {
	// WithTx runs fn with [Stores] that all work inside a single transaction. The
	// transaction is committed if fn returns nil and rolled back if fn returns an
	// error or the commit fails.
	//
	// Returns the following errors:
	//   - the error of fn - unchanged, if fn fails.
	//   - an error - if the transaction could not be started or committed.
	WithTx(ctx context.Context, fn func(Stores) error) error
}
