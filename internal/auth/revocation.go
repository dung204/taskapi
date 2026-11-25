package auth

import (
	"context"
	"log/slog"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/metrics"
)

// Revoker rejects access tokens that were issued before they expire. It only
// needs to remember a revocation for as long as an access token lives.
type Revoker interface {
	// RevokeSession makes the access tokens of the session sid invalid.
	//
	// Returns the following errors:
	//   - an error - if the revocation could not be recorded (the fail-open
	//     wrapper logs and ignores it).
	RevokeSession(ctx context.Context, sid uuid.UUID) error

	// RevokeUser makes every access token of the user issued before at invalid.
	//
	// Returns the following errors:
	//   - an error - if the revocation could not be recorded (the fail-open
	//     wrapper logs and ignores it).
	RevokeUser(ctx context.Context, userID uuid.UUID, at time.Time) error
}

// failOpenRevoker makes revocation best-effort: a failing backend is logged and
// then ignored, so the caller still succeeds.
type failOpenRevoker struct {
	next    Revoker
	logger  *slog.Logger
	metrics *metrics.Auth
}

// NewFailOpenRevoker wraps next so that its errors are logged and swallowed.
//
// Use it only where the database is already authoritative for the change (the
// session is deleted, tokens_valid_after is set) and next is a fast path for
// rejecting access tokens that were already issued. When next fails, those
// tokens stay valid until they expire. Do not use it where a missed revocation
// must fail the request.
func NewFailOpenRevoker(next Revoker, logger *slog.Logger, metrics *metrics.Auth) Revoker {
	if logger == nil {
		logger = slog.Default()
	}
	return &failOpenRevoker{next: next, logger: logger, metrics: metrics}
}

func (r *failOpenRevoker) RevokeSession(ctx context.Context, sid uuid.UUID) error {
	if err := r.next.RevokeSession(ctx, sid); err != nil {
		r.metrics.IncRevokeFailed(metrics.RevokeOpSession)
		r.logger.WarnContext(ctx, "revoke session failed, access token stays valid until it expires",
			"session_id", sid, "error", err,
		)
	}
	return nil
}

func (r *failOpenRevoker) RevokeUser(ctx context.Context, userID uuid.UUID, at time.Time) error {
	if err := r.next.RevokeUser(ctx, userID, at); err != nil {
		r.metrics.IncRevokeFailed(metrics.RevokeOpUser)
		r.logger.WarnContext(ctx, "revoke user failed, access tokens stay valid until they expire",
			"user_id", userID, "error", err)
	}
	return nil
}
