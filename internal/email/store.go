package email

import (
	"context"
	"time"
	"uuid"
)

type ClaimedItem struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Kind     Kind
	ToEmail  string
	Attempts int32
}

type OutboxStore interface {
	Create(ctx context.Context, item OutboxItem) error
	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int32) ([]ClaimedItem, error)
	MarkSent(ctx context.Context, id uuid.UUID, at time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, at time.Time, lastError string) error
	Reschedule(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastError string) error
	DeleteSent(ctx context.Context, before time.Time, limit int32) (int64, error)
}
