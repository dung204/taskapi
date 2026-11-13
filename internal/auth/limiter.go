package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/dung204/taskapi/internal/metrics"
)

type LoginLimiter interface {
	Attempt(ctx context.Context, email string) (wait time.Duration, err error)
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

type MailRequestLimiter interface {
	Allow(ctx context.Context, email string) (retryAfter time.Duration, err error)
}

type failOpenMailRequestLimiter struct {
	next    MailRequestLimiter
	logger  *slog.Logger
	metrics *metrics.Auth
}

var _ MailRequestLimiter = (*failOpenMailRequestLimiter)(nil)

func NewFailOpenMailRequestLimiter(next MailRequestLimiter, logger *slog.Logger, metrics *metrics.Auth) *failOpenMailRequestLimiter {
	if logger == nil {
		logger = slog.Default()
	}

	return &failOpenMailRequestLimiter{
		next:    next,
		logger:  logger,
		metrics: metrics,
	}
}

// Allow implements [MailRequestLimiter].
func (l *failOpenMailRequestLimiter) Allow(ctx context.Context, email string) (retryAfter time.Duration, err error) {
	ms, err := l.next.Allow(ctx, email)
	if err != nil {
		l.metrics.LimiterFailOpenInc(metrics.LimiterMail)
		l.logger.WarnContext(ctx, "allow mail request failed", "error", err)
		return 0, nil
	}
	return ms, nil
}
