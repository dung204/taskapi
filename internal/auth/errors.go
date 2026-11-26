package auth

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/email"
)

// Credentials and passwords.
var (
	ErrInvalidCredentials   = errors.New("invalid email or password")
	ErrInvalidHash          = errors.New("invalid hash")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrInvalidEmail         = email.ErrInvalidEmail
	ErrInvalidPassword      = fmt.Errorf("%w: invalid password", apperr.ErrInvalidInput)
	ErrWrongCurrentPassword = errors.New("wrong current password")
)

// Access tokens (JWT) and one-time user tokens.
var (
	ErrInvalidToken                  = errors.New("token is invalid")
	ErrExpiredToken                  = errors.New("token is expired")
	ErrInvalidRefreshToken           = errors.New("invalid refresh token")
	ErrInvalidRefreshTokenNeedLogout = fmt.Errorf("%w: unusable", ErrInvalidRefreshToken)
	ErrInvalidPurpose                = fmt.Errorf("%w: purpose must be one of the following: %s", apperr.ErrInvalidInput, strings.Join(allowedPurposes, ", "))
	ErrInvalidTokenMode              = fmt.Errorf(`%w: token_mode must be one of the following: "token", "cookie"`, apperr.ErrInvalidInput)
)

// Sessions.
var (
	ErrSessionNotFound   = errors.New("session not found")
	ErrSessionNotRotated = errors.New("session not rotated")
)

// Users and accounts.
var (
	ErrUserNotFound     = errors.New("user not found")
	ErrEmailTaken       = errors.New("email is taken")
	ErrEmailNotVerified = errors.New("email not verified")
	ErrAccountDisabled  = errors.New("account disabled")
)

// User tokens.
var (
	ErrUserTokenInvalid = errors.New("user token is invalid")
)
