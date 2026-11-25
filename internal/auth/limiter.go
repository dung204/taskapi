package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/dung204/taskapi/internal/metrics"
)

// LoginLimiter slows down password guessing per email: a few attempts are free,
// then each further attempt locks the email for a growing time.
type LoginLimiter interface {
	// Attempt records a login attempt for email. While the email is locked, the
	// attempt is refused and not counted.
	//
	// Returns:
	//   - wait - how long the caller must wait before trying again; zero when the
	//     attempt may go ahead.
	//
	// Returns the following errors:
	//   - an error - if the limiter backend is unavailable (the fail-open wrapper
	//     logs it and allows the attempt).
	Attempt(ctx context.Context, email string) (wait time.Duration, err error)

	// Reset forgets the attempts and the lock of email, such as after a
	// successful login.
	//
	// Returns the following errors:
	//   - an error - if the limiter backend is unavailable (the fail-open wrapper
	//     logs and ignores it).
	Reset(ctx context.Context, email string) error
}

type failOpenLoginLimiter struct {
	next    LoginLimiter
	logger  *slog.Logger
	metrics *metrics.Auth
}

var _ LoginLimiter = (*failOpenLoginLimiter)(nil)

func NewFailOpenLoginLimiter(next LoginLimiter, logger *slog.Logger, metrics *metrics.Auth) *failOpenLoginLimiter {
	if logger == nil {
		logger = slog.Default()
	}

	return &failOpenLoginLimiter{
		next:    next,
		logger:  logger,
		metrics: metrics,
	}
}

// Attempt implements [LoginLimiter].
func (l *failOpenLoginLimiter) Attempt(ctx context.Context, email string) (wait time.Duration, err error) {
	ms, err := l.next.Attempt(ctx, email)
	if err != nil {
		l.metrics.LimiterFailOpenInc(metrics.LimiterLogin)
		l.logger.WarnContext(ctx, "attempt to login failed", "error", err)
		return 0, nil
	}
	return ms, nil
}

// Reset implements [LoginLimiter].
func (l *failOpenLoginLimiter) Reset(ctx context.Context, email string) error {
	if err := l.next.Reset(ctx, email); err != nil {
		l.metrics.LimiterFailOpenInc(metrics.LimiterLogin)
		l.logger.WarnContext(ctx, "reset login attempts failed", "error", err)
	}
	return nil
}

// AuthMailLimiter limits how often an email can be asked to receive an
// email (verification, password reset): a cooldown between two requests and a
// maximum number per hour.
type AuthMailLimiter interface {
	// Allow checks whether another email may be sent to email now and, if so,
	// starts the cooldown.
	//
	// Returns:
	//   - retryAfter - how long to wait before asking again when the request is
	//     refused (cooldown running or hourly maximum reached); zero when it is
	//     allowed.
	//
	// Returns the following errors:
	//   - an error - if the limiter backend is unavailable (the fail-open wrapper
	//     logs it and allows the request).
	Allow(ctx context.Context, email string) (retryAfter time.Duration, err error)
}

type failOpenAuthMailLimiter struct {
	next    AuthMailLimiter
	logger  *slog.Logger
	metrics *metrics.Auth
}

var _ AuthMailLimiter = (*failOpenAuthMailLimiter)(nil)

func NewFailOpenAuthMailLimiter(next AuthMailLimiter, logger *slog.Logger, metrics *metrics.Auth) *failOpenAuthMailLimiter {
	if logger == nil {
		logger = slog.Default()
	}

	return &failOpenAuthMailLimiter{
		next:    next,
		logger:  logger,
		metrics: metrics,
	}
}

// Allow implements [AuthMailLimiter].
func (l *failOpenAuthMailLimiter) Allow(ctx context.Context, email string) (retryAfter time.Duration, err error) {
	ms, err := l.next.Allow(ctx, email)
	if err != nil {
		l.metrics.LimiterFailOpenInc(metrics.LimiterMail)
		l.logger.WarnContext(ctx, "allow mail request failed", "error", err)
		return 0, nil
	}
	return ms, nil
}
