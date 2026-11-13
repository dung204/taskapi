package auth

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/apperr"
)

func TestTokenPurpose_IsValid(t *testing.T) {
	tests := []struct {
		purpose TokenPurpose
		want    bool
	}{
		{PurposeVerifyEmail, true},
		{PurposeResetPassword, true},
		{TokenPurpose("verify_email"), true},
		{TokenPurpose(""), false},
		{TokenPurpose("unknown"), false},
		{TokenPurpose("VERIFY_EMAIL"), false},
		{TokenPurpose(" verify_email"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.purpose), func(t *testing.T) {
			if got := tt.purpose.IsValid(); got != tt.want {
				t.Errorf("TokenPurpose(%q).IsValid() = %v, want %v", tt.purpose, got, tt.want)
			}
		})
	}
}

func TestNewUserToken(t *testing.T) {
	// non-UTC zone with sub-microsecond precision
	now := time.Date(2026, 10, 1, 19, 0, 0, 123456789, time.FixedZone("UTC+7", 7*3600))
	wantCreatedAt := time.Date(2026, 10, 1, 12, 0, 0, 123456000, time.UTC)

	userID := uuid.NewV7()
	hash := sha256.Sum256([]byte("token"))

	// the caller decides the lifetime; ExpiresAt is CreatedAt plus the TTL, so it
	// is normalized like CreatedAt (UTC, microseconds)
	ttl := 36 * time.Hour
	wantExpiresAt := time.Date(2026, 10, 3, 0, 0, 0, 123456000, time.UTC)

	tests := []struct {
		name    string
		purpose TokenPurpose
	}{
		{name: "verify email", purpose: PurposeVerifyEmail},
		{name: "reset password", purpose: PurposeResetPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewUserToken(NewUserTokenParams{UserID: userID, Purpose: tt.purpose, TokenHash: hash[:], TTL: ttl}, now)
			if err != nil {
				t.Fatalf("NewUserToken() unexpected error: %v", err)
			}

			if got.ID == uuid.Nil() {
				t.Error("ID is the nil UUID")
			}
			if got.UserID != userID {
				t.Errorf("UserID = %v, want %v", got.UserID, userID)
			}
			if got.Purpose != tt.purpose {
				t.Errorf("Purpose = %q, want %q", got.Purpose, tt.purpose)
			}
			if !bytes.Equal(got.TokenHash, hash[:]) {
				t.Errorf("TokenHash = %x, want %x", got.TokenHash, hash)
			}
			if got.UsedAt != nil {
				t.Errorf("UsedAt = %v, want nil", got.UsedAt)
			}
			if !got.CreatedAt.Equal(wantCreatedAt) || got.CreatedAt.Location() != time.UTC {
				t.Errorf("CreatedAt = %v, want %v (UTC, microseconds)", got.CreatedAt, wantCreatedAt)
			}
			if !got.ExpiresAt.Equal(wantExpiresAt) || got.ExpiresAt.Location() != time.UTC {
				t.Errorf("ExpiresAt = %v, want %v (UTC, microseconds)", got.ExpiresAt, wantExpiresAt)
			}
		})
	}
}

func TestNewUserToken_Invalid(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	validHash := make([]byte, sha256.Size)

	tests := []struct {
		name    string
		params  NewUserTokenParams
		wantErr error // nil means any error
	}{
		{
			name:    "empty purpose",
			params:  NewUserTokenParams{Purpose: "", TokenHash: validHash},
			wantErr: ErrInvalidPurpose,
		},
		{
			name:    "unknown purpose",
			params:  NewUserTokenParams{Purpose: "unknown", TokenHash: validHash},
			wantErr: ErrInvalidPurpose,
		},
		{
			// the purpose is checked before the hash
			name:    "unknown purpose and invalid hash",
			params:  NewUserTokenParams{Purpose: "unknown", TokenHash: nil},
			wantErr: ErrInvalidPurpose,
		},
		{name: "nil hash", params: NewUserTokenParams{Purpose: PurposeVerifyEmail, TokenHash: nil}},
		{name: "empty hash", params: NewUserTokenParams{Purpose: PurposeVerifyEmail, TokenHash: []byte{}}},
		{name: "hash too short", params: NewUserTokenParams{Purpose: PurposeVerifyEmail, TokenHash: make([]byte, sha256.Size-1)}},
		{name: "hash too long", params: NewUserTokenParams{Purpose: PurposeResetPassword, TokenHash: make([]byte, sha256.Size+1)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewUserToken(tt.params, now)

			if err == nil {
				t.Fatal("NewUserToken() error = nil, want an error")
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("NewUserToken() error = %v, want %v", err, tt.wantErr)
			}
			if got.ID != uuid.Nil() {
				t.Errorf("NewUserToken() = %+v, want zero value on error", got)
			}
		})
	}
}

func TestErrInvalidPurpose_IsInvalidInput(t *testing.T) {
	if !errors.Is(ErrInvalidPurpose, apperr.ErrInvalidInput) {
		t.Error("ErrInvalidPurpose does not wrap apperr.ErrInvalidInput")
	}
}

func TestNewUserToken_UniqueIDs(t *testing.T) {
	p := NewUserTokenParams{UserID: uuid.NewV7(), Purpose: PurposeVerifyEmail, TokenHash: make([]byte, sha256.Size)}

	a, err := NewUserToken(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewUserToken(p, time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if a.ID == b.ID {
		t.Errorf("two tokens share the ID %v", a.ID)
	}
}
