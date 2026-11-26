package task

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/metrics"
)

// InviteMailLimiter limits how often an invitation email can be sent to one
// address: a cooldown between two emails and a maximum number per hour.
type InviteMailLimiter interface {
	// Allow checks whether another invitation email may be sent to email now
	// and, if so, starts the cooldown.
	//
	// Returns:
	//   - retryAfter - how long to wait before sending again when the email is
	//     refused; zero when it is allowed.
	//
	// Returns the following errors:
	//   - an error - if the limiter backend is unavailable (the fail-open wrapper
	//     logs it and allows the email).
	Allow(ctx context.Context, email string) (retryAfter time.Duration, err error)
}

// OwnerInviteLimiter limits how many invitations one task owner can send per
// hour.
type OwnerInviteLimiter interface {
	// Allow checks whether owner may send another invitation now and, if so,
	// counts it.
	//
	// Returns:
	//   - retryAfter - how long to wait before inviting again when the owner is
	//     over the limit; zero when it is allowed.
	//
	// Returns the following errors:
	//   - an error - if the limiter backend is unavailable (the fail-open wrapper
	//     logs it and allows the invitation).
	Allow(ctx context.Context, owner uuid.UUID) (retryAfter time.Duration, err error)
}

type failOpenInviteMailLimiter struct {
	next    InviteMailLimiter
	logger  *slog.Logger
	metrics *metrics.Task
}

var _ InviteMailLimiter = (*failOpenInviteMailLimiter)(nil)

// NewFailOpenInviteMailLimiter wraps next so that its errors are logged,
// counted and swallowed: the invitation email is allowed.
func NewFailOpenInviteMailLimiter(next InviteMailLimiter, logger *slog.Logger, metrics *metrics.Task) *failOpenInviteMailLimiter {
	if logger == nil {
		logger = slog.Default()
	}

	return &failOpenInviteMailLimiter{next: next, logger: logger, metrics: metrics}
}

// Allow implements [InviteMailLimiter].
func (l *failOpenInviteMailLimiter) Allow(ctx context.Context, email string) (retryAfter time.Duration, err error) {
	wait, err := l.next.Allow(ctx, email)
	if err != nil {
		l.metrics.LimiterFailOpenInc(metrics.LimiterInviteMail)
		l.logger.WarnContext(ctx, "allow invitation mail failed", "error", err)
		return 0, nil
	}
	return wait, nil
}

type failOpenOwnerInviteLimiter struct {
	next    OwnerInviteLimiter
	logger  *slog.Logger
	metrics *metrics.Task
}

var _ OwnerInviteLimiter = (*failOpenOwnerInviteLimiter)(nil)

// NewFailOpenOwnerInviteLimiter wraps next so that its errors are logged,
// counted and swallowed: the invitation is allowed.
func NewFailOpenOwnerInviteLimiter(next OwnerInviteLimiter, logger *slog.Logger, metrics *metrics.Task) *failOpenOwnerInviteLimiter {
	if logger == nil {
		logger = slog.Default()
	}

	return &failOpenOwnerInviteLimiter{next: next, logger: logger, metrics: metrics}
}

// Allow implements [OwnerInviteLimiter].
func (l *failOpenOwnerInviteLimiter) Allow(ctx context.Context, owner uuid.UUID) (retryAfter time.Duration, err error) {
	wait, err := l.next.Allow(ctx, owner)
	if err != nil {
		l.metrics.LimiterFailOpenInc(metrics.LimiterInviteOwner)
		l.logger.WarnContext(ctx, "allow owner invitation failed", "error", err)
		return 0, nil
	}
	return wait, nil
}
