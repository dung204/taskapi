package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/metrics"
)

// revocationChecker tells whether an access token was revoked (see
// [auth.Revoker]).
type revocationChecker interface {
	// Check looks up the revocations of the session sid and of the user.
	//
	// Returns:
	//   - revoked - whether a token of the session, issued at iat, was revoked,
	//     either with its session or with all tokens of the user issued before
	//     the user's cutoff.
	//
	// Returns the following errors:
	//   - an error - if the revocation backend could not be read.
	Check(ctx context.Context, sid, userID uuid.UUID, iat time.Time) (revoked bool, err error)
}

// tokenVerifier checks access tokens.
type tokenVerifier interface {
	// Verify checks the signature, issuer, audience and expiry of token at now.
	//
	// Returns:
	//   - claims - the [auth.Claims] of the token: user, session and issue time.
	//
	// Returns the following errors:
	//   - [auth.ErrExpiredToken] - if the token has expired.
	//   - [auth.ErrInvalidToken] - if the token is malformed, wrongly signed, for
	//     another issuer or audience, or lacks a valid user, session or time.
	Verify(token string, now time.Time) (claims auth.Claims, err error)
}

type Identity struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

type identityKey struct{}

type AuthMiddlewareDeps struct {
	Checker  revocationChecker
	Verifier tokenVerifier
	Logger   *slog.Logger     // nillable, defaults to slog.Default()
	Metrics  *metrics.Auth    // nillable
	Now      func() time.Time // nillable, defaults to time.Now
}

func authMiddleware(deps AuthMiddlewareDeps) middleware {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The generated wrapper sets this key only for operations that
			// require security, so public routes (security: []) pass through.
			if r.Context().Value(oapigen.BearerAuthScopes) == nil {
				next.ServeHTTP(w, r)
				return
			}

			scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
				writeUnauthorized(w)
				return
			}

			claims, err := deps.Verifier.Verify(token, deps.Now())
			if err != nil {
				writeUnauthorized(w)
				return
			}

			start := time.Now()
			revoked, err := deps.Checker.Check(r.Context(), claims.SessionID, claims.UserID, claims.IssuedAt)
			deps.Metrics.ObserveRevocationCheck(time.Since(start))

			switch {
			case err != nil:
				deps.Metrics.RevocationFailOpenInc(failOpenReason(err))
				deps.Logger.WarnContext(r.Context(), "revocation check failed", "error", err)
			case revoked:
				writeUnauthorized(w)
				return
			}

			ctx := context.WithValue(r.Context(), identityKey{}, Identity{UserID: claims.UserID, SessionID: claims.SessionID})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func identityFrom(ctx context.Context) Identity {
	identity, _ := ctx.Value(identityKey{}).(Identity)
	return identity
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	writeError(w, http.StatusUnauthorized, apperr.ErrUnauthorized.Error())
}

func failOpenReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return metrics.FailOpenTimeout
	}
	if ne, ok := errors.AsType[net.Error](err); ok && ne.Timeout() {
		return metrics.FailOpenTimeout
	}
	return metrics.FailOpenError
}
