package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"
	"uuid"
)

type Claims struct {
	UserID, SessionID uuid.UUID
	IssuedAt          time.Time
}

type TokenIssuer interface {
	Issue(userID, sessionID uuid.UUID, now time.Time) (string, time.Time, error)
}

func decodeOpaqueToken(token string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != sha256.Size {
		return nil, ErrInvalidToken
	}
	return raw, nil
}

func generateOpaqueToken() ([]byte, error) {
	token := make([]byte, sha256.Size)
	if _, err := rand.Read(token); err != nil {
		return nil, fmt.Errorf("generate opaque token: %w", err)
	}
	return token, nil
}
