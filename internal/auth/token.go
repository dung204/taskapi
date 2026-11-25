package auth

import (
	"time"
	"uuid"
)

type Claims struct {
	UserID, SessionID uuid.UUID
	IssuedAt          time.Time
}

// AccessTokenIssuer creates the short-lived access tokens of logged-in users.
type AccessTokenIssuer interface {
	// Issue creates an access token for userID, bound to sessionID and issued at
	// now.
	//
	// Returns:
	//   - token - the signed access token.
	//   - expiresAt - the time at which the token expires.
	//
	// Returns the following errors:
	//   - an error - if the token could not be signed.
	Issue(userID, sessionID uuid.UUID, now time.Time) (token string, expiresAt time.Time, err error)
}
