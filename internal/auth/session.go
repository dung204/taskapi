package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

type Session struct {
	ID                       uuid.UUID
	UserID                   uuid.UUID
	RefreshTokenHash         []byte
	PreviousRefreshTokenHash []byte
	RotatedAt                *time.Time
	ExpiresAt                time.Time
	CreatedAt                time.Time
	UserAgent                *string
}

type NewSessionParams struct {
	UserID           uuid.UUID
	RefreshTokenHash []byte
	UserAgent        *string
	TTL              time.Duration
}

func NewSession(p NewSessionParams, now time.Time) (Session, error) {
	id := uuid.NewV7()
	createdAt := now.UTC().Truncate(time.Microsecond)
	expiresAt := createdAt.Add(p.TTL)

	if len(p.RefreshTokenHash) != sha256.Size {
		return Session{}, fmt.Errorf("new session: refresh token hash must be %d bytes, got %d", sha256.Size, len(p.RefreshTokenHash))
	}

	if p.UserAgent != nil {
		*p.UserAgent = sanitizeUserAgent(*p.UserAgent, 200)
	}

	return Session{
		ID:               id,
		UserID:           p.UserID,
		RefreshTokenHash: p.RefreshTokenHash,
		ExpiresAt:        expiresAt,
		CreatedAt:        createdAt,
		UserAgent:        p.UserAgent,
	}, nil
}

func sanitizeUserAgent(ua string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}

	if strings.IndexByte(ua, 0) != -1 {
		ua = strings.ReplaceAll(ua, "\x00", "")
	}

	if !utf8.ValidString(ua) {
		ua = strings.ToValidUTF8(ua, "")
	}

	if len(ua) <= maxChars {
		return strings.TrimSpace(ua)
	}

	count := 0
	for i := range ua {
		if count == maxChars {
			return strings.TrimSpace(ua[:i])
		}
		count++
	}

	return strings.TrimSpace(ua)
}

func (s Session) IsExpired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

type RefreshMatch int

const (
	RefreshNone RefreshMatch = iota
	RefreshCurrent
	RefreshGrace
	RefreshReuse
)

func (s Session) MatchRefresh(hash []byte, now time.Time, grace time.Duration) RefreshMatch {
	switch {
	case subtle.ConstantTimeCompare(hash, s.RefreshTokenHash) == 1:
		return RefreshCurrent
	case len(s.PreviousRefreshTokenHash) > 0 &&
		subtle.ConstantTimeCompare(hash, s.PreviousRefreshTokenHash) == 1:
		if s.RotatedAt != nil && now.Sub(*s.RotatedAt) <= grace {
			return RefreshGrace
		}
		return RefreshReuse
	default:
		return RefreshNone
	}
}
