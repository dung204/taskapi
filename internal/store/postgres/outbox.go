package postgres

import (
	"context"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
)

type OutboxStore struct {
	q *sqlcgen.Queries
}

var _ email.OutboxStore = (*OutboxStore)(nil)

func NewOutboxStore(q *sqlcgen.Queries) *OutboxStore {
	return &OutboxStore{q: q}
}

// Claim implements [email.OutboxStore].
func (s *OutboxStore) Claim(ctx context.Context, now time.Time, lease time.Duration, limit int32) ([]email.ClaimedItem, error) {
	formatError := func(err error) error {
		return formatPostgresError("claim", "outbox items", "", err)
	}

	rows, err := s.q.ClaimOutboxItems(ctx, sqlcgen.ClaimOutboxItemsParams{
		LockedUntil: new(now.Add(lease)), Now: now, Limit: limit,
	})
	if err != nil {
		return nil, formatError(err)
	}

	items := make([]email.ClaimedItem, len(rows))
	for i, r := range rows {
		items[i] = email.ClaimedItem(r)
	}
	return items, nil
}

// Create implements [email.OutboxStore].
func (s *OutboxStore) Create(ctx context.Context, item email.OutboxItem) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "outbox item", "", err)
	}

	if err := s.q.CreateOutboxItem(ctx, sqlcgen.CreateOutboxItemParams(item)); err != nil {
		return formatError(err)
	}

	return nil
}

// DeleteSent implements [email.OutboxStore].
func (s *OutboxStore) DeleteSent(ctx context.Context, before time.Time, limit int32) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("delete", "sent outbox items", "", err)
	}

	affected, err := s.q.DeleteSentOutboxItems(ctx, sqlcgen.DeleteSentOutboxItemsParams{
		Before: &before, Limit: limit,
	})
	if err != nil {
		return 0, formatError(err)
	}

	return affected, nil
}

// MarkFailed implements [email.OutboxStore].
func (s *OutboxStore) MarkFailed(ctx context.Context, id uuid.UUID, at time.Time, lastError string) error {
	formatError := func(err error) error {
		return formatPostgresError("mark failed", "outbox item", "", err)
	}

	if err := s.q.MarkOutboxItemFailed(ctx, sqlcgen.MarkOutboxItemFailedParams{FailedAt: &at, LastError: &lastError, ID: id}); err != nil {
		return formatError(err)
	}

	return nil
}

// MarkSent implements [email.OutboxStore].
func (s *OutboxStore) MarkSent(ctx context.Context, id uuid.UUID, at time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "outbox item", "", err)
	}

	if err := s.q.MarkOutboxItemSent(ctx, sqlcgen.MarkOutboxItemSentParams{SentAt: &at, ID: id}); err != nil {
		return formatError(err)
	}

	return nil
}

// Reschedule implements [email.OutboxStore].
func (s *OutboxStore) Reschedule(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastError string) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "outbox item", "", err)
	}

	if err := s.q.RescheduleOutboxItem(ctx, sqlcgen.RescheduleOutboxItemParams{NextAttemptAt: nextAttemptAt, LastError: &lastError, ID: id}); err != nil {
		return formatError(err)
	}

	return nil
}
