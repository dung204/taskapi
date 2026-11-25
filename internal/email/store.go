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
	RefID    *uuid.UUID
}

// OutboxStore persists the emails waiting to be delivered. A worker claims due
// items, sends them, and then reports the outcome with [OutboxStore.MarkSent],
// [OutboxStore.MarkFailed] or [OutboxStore.Reschedule].
type OutboxStore interface {
	// Create persists an [OutboxItem] to be delivered later.
	Create(ctx context.Context, item OutboxItem) error

	// Claim leases up to limit items to the caller until now+lease, earliest due
	// first. An item is claimable when it is neither sent nor failed, its next
	// attempt time is at or before now, and it is not leased (or its lease has
	// run out). Each claim counts as an attempt. Concurrent callers never
	// receive the same item.
	//
	// Returns:
	//   - items - the claimed [ClaimedItem]s, whose Attempts includes the current
	//     attempt.
	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int32) (items []ClaimedItem, err error)

	// MarkSent records that the item was delivered at at and releases its lease.
	// It does nothing if the item is unknown or already sent.
	MarkSent(ctx context.Context, id uuid.UUID, at time.Time) error

	// MarkFailed gives up on the item for good: it records at and lastError,
	// releases its lease, and the item is never claimed again. It does nothing if
	// the item is unknown, already sent or already failed.
	MarkFailed(ctx context.Context, id uuid.UUID, at time.Time, lastError string) error

	// Reschedule makes the item claimable again from nextAttemptAt, records
	// lastError and releases its lease. It does nothing if the item is unknown or
	// already sent.
	Reschedule(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastError string) error

	// DeleteSent removes up to limit items sent before the given time.
	//
	// Returns:
	//   - deleted - the number of items removed.
	DeleteSent(ctx context.Context, before time.Time, limit int32) (deleted int64, err error)
}
