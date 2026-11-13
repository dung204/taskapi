package auth

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"time"
	"uuid"
)

type TokenPurpose string

const (
	PurposeVerifyEmail   TokenPurpose = "verify_email"
	PurposeResetPassword TokenPurpose = "reset_password"
)

var allowedPurposes = []string{
	string(PurposeVerifyEmail),
	string(PurposeResetPassword),
}

func (p TokenPurpose) IsValid() bool {
	return slices.Contains(allowedPurposes, string(p))
}

type UserToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Purpose   TokenPurpose
	TokenHash []byte
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

type NewUserTokenParams struct {
	UserID    uuid.UUID
	Purpose   TokenPurpose
	TokenHash []byte
	TTL       time.Duration
}

func NewUserToken(p NewUserTokenParams, now time.Time) (UserToken, error) {
	id := uuid.NewV7()
	createdAt := now.UTC().Truncate(time.Microsecond)

	if !p.Purpose.IsValid() {
		return UserToken{}, ErrInvalidPurpose
	}

	if len(p.TokenHash) != sha256.Size {
		return UserToken{}, fmt.Errorf("new user token: token hash must be %d bytes, got %d", sha256.Size, len(p.TokenHash))
	}

	return UserToken{
		ID:        id,
		UserID:    p.UserID,
		Purpose:   p.Purpose,
		TokenHash: p.TokenHash,
		ExpiresAt: createdAt.Add(p.TTL),
		CreatedAt: createdAt,
	}, nil
}
