package email

import (
	"errors"
	"fmt"
	"slices"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/apperr"
)

type Kind string

const (
	KindVerifyEmail   Kind = "verify_email"
	KindResetPassword Kind = "reset_password"
	// KindAlreadyRegistered tells the owner of an existing account that someone
	// tried to register with their address. It carries no token.
	KindAlreadyRegistered Kind = "already_registered"
)

var allowedKinds = []string{
	string(KindVerifyEmail),
	string(KindResetPassword),
	string(KindAlreadyRegistered),
}

func (k Kind) IsValid() bool {
	return slices.Contains(allowedKinds, string(k))
}

type OutboxItem struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	Kind          Kind
	ToEmail       string
	Attempts      int32
	NextAttemptAt time.Time
	LockedUntil   *time.Time
	SentAt        *time.Time
	FailedAt      *time.Time
	LastError     *string
	CreatedAt     time.Time
}

type NewOutboxItemParams struct {
	UserID  uuid.UUID
	Kind    Kind
	ToEmail string
}

func NewOutboxItem(p NewOutboxItemParams, now time.Time) (OutboxItem, error) {
	id := uuid.NewV7()
	createdAt := now.UTC().Truncate(time.Microsecond)

	if p.UserID == uuid.Nil() {
		return OutboxItem{}, errors.New("new outbox item: user id must not be nil")
	}

	if !p.Kind.IsValid() {
		return OutboxItem{}, ErrInvalidKind
	}

	if len(p.ToEmail) == 0 {
		return OutboxItem{}, fmt.Errorf("%w: to email must not be empty", apperr.ErrInvalidInput)
	}

	return OutboxItem{
		ID:            id,
		UserID:        p.UserID,
		Kind:          p.Kind,
		ToEmail:       p.ToEmail,
		Attempts:      0,
		NextAttemptAt: createdAt,
		CreatedAt:     createdAt,
	}, nil
}
