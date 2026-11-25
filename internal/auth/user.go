package auth

import (
	"fmt"
	"time"
	"unicode/utf8"
	"uuid"

	"golang.org/x/text/unicode/norm"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/email"
)

type User struct {
	ID               uuid.UUID
	Email            string
	PasswordHash     *string
	EmailVerifiedAt  *time.Time
	DisabledAt       *time.Time
	TokensValidAfter *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type NewUserParams struct {
	Email        string
	PasswordHash *string
}

func NewUser(params NewUserParams, now time.Time) (User, error) {
	id := uuid.NewV7()
	nowUTC := now.UTC().Truncate(time.Microsecond)

	email, err := email.Parse(params.Email)
	if err != nil {
		return User{}, fmt.Errorf("%w: %w", apperr.ErrInvalidInput, err)
	}

	return User{
		ID:           id,
		Email:        email,
		PasswordHash: params.PasswordHash,
		CreatedAt:    nowUTC,
		UpdatedAt:    nowUTC,
	}, nil
}

func (u *User) ChangePassword(hash string, now time.Time) {
	nowUTC := now.UTC().Truncate(time.Microsecond)
	u.PasswordHash = &hash
	u.UpdatedAt = nowUTC
}

func (u *User) Disable(now time.Time) {
	if u.DisabledAt != nil {
		return
	}

	nowUTC := now.UTC().Truncate(time.Microsecond)
	u.DisabledAt = &nowUTC
	u.UpdatedAt = nowUTC
}

func (u *User) CanLogin() error {
	switch {
	case u.EmailVerifiedAt == nil:
		return ErrEmailNotVerified
	case u.DisabledAt != nil:
		return ErrAccountDisabled
	default:
		return nil
	}
}

func normalizePassword(password string) (string, error) {
	if !utf8.ValidString(password) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidPassword)
	}
	return norm.NFKC.String(password), nil
}

const (
	minPasswordLen = 8
	maxPasswordLen = 128
)

// parseLoginPassword normalizes a password given at login. Unlike
// parsePassword it has no minimum length (a too-short password is just a wrong
// one, so it gets 401 like any other); it only rejects what can never be a
// valid password: empty, not UTF-8, or longer than maxPasswordLen after NFKC.
// The upper bound also keeps the hasher from working on huge input.
func parseLoginPassword(password string) (string, error) {
	normalized, err := normalizePassword(password)
	if err != nil {
		return "", err
	}

	if n := utf8.RuneCountInString(normalized); n == 0 || n > maxPasswordLen {
		return "", fmt.Errorf("%w: must not be empty and must have at most %d characters", ErrInvalidPassword, maxPasswordLen)
	}
	return normalized, nil
}

func parsePassword(password string) (string, error) {
	normalized, err := normalizePassword(password)
	if err != nil {
		return "", err
	}

	if n := utf8.RuneCountInString(normalized); n < minPasswordLen || n > maxPasswordLen {
		return "", fmt.Errorf("%w: length must be between %d and %d", ErrInvalidPassword, minPasswordLen, maxPasswordLen)
	}
	return normalized, nil
}
