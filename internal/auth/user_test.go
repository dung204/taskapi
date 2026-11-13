package auth

import (
	"errors"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/dung204/taskapi/internal/apperr"
)

var testUserCreatedAt, _ = time.Parse(time.RFC3339, "2026-10-01T00:00:00.000Z")
var testUserNow = testUserCreatedAt.Add(30 * time.Second)

func utc(t time.Time) time.Time {
	return t.UTC().Truncate(time.Microsecond)
}

func defaultUser() User {
	return User{
		Email:     "user@example.com",
		CreatedAt: utc(testUserCreatedAt),
		UpdatedAt: utc(testUserCreatedAt),
	}
}

func userWith(mod func(*User)) User {
	u := defaultUser()
	mod(&u)
	return u
}

func TestNewUser(t *testing.T) {
	// a time with sub-microsecond precision and a non-UTC zone
	nowWithNanos := time.Date(2026, 10, 1, 7, 0, 0, 123456789, time.FixedZone("UTC+7", 7*3600))

	tests := []struct {
		name     string
		params   NewUserParams
		now      time.Time
		wantUser User
		wantErr  error
	}{
		{
			name:   "valid email",
			params: NewUserParams{Email: "user@example.com"},
			now:    testUserCreatedAt,
			wantUser: User{
				Email:     "user@example.com",
				CreatedAt: utc(testUserCreatedAt),
				UpdatedAt: utc(testUserCreatedAt),
			},
		},
		{
			name:   "email is trimmed and lowercased",
			params: NewUserParams{Email: "  User.Name+Tag@Example.COM \n"},
			now:    testUserCreatedAt,
			wantUser: User{
				Email:     "user.name+tag@example.com",
				CreatedAt: utc(testUserCreatedAt),
				UpdatedAt: utc(testUserCreatedAt),
			},
		},
		{
			name:   "subdomain email",
			params: NewUserParams{Email: "user@mail.example.co.uk"},
			now:    testUserCreatedAt,
			wantUser: User{
				Email:     "user@mail.example.co.uk",
				CreatedAt: utc(testUserCreatedAt),
				UpdatedAt: utc(testUserCreatedAt),
			},
		},
		{
			name:   "254 characters",
			params: NewUserParams{Email: strings.Repeat("a", 254-len("@example.com")) + "@example.com"},
			now:    testUserCreatedAt,
			wantUser: User{
				Email:     strings.Repeat("a", 254-len("@example.com")) + "@example.com",
				CreatedAt: utc(testUserCreatedAt),
				UpdatedAt: utc(testUserCreatedAt),
			},
		},
		{
			name:   "password hash is kept",
			params: NewUserParams{Email: "user@example.com", PasswordHash: new("hash")},
			now:    testUserCreatedAt,
			wantUser: User{
				Email:        "user@example.com",
				PasswordHash: new("hash"),
				CreatedAt:    utc(testUserCreatedAt),
				UpdatedAt:    utc(testUserCreatedAt),
			},
		},
		{
			name:   "time is normalized to UTC and truncated to microseconds",
			params: NewUserParams{Email: "user@example.com"},
			now:    nowWithNanos,
			wantUser: User{
				Email:     "user@example.com",
				CreatedAt: utc(nowWithNanos),
				UpdatedAt: utc(nowWithNanos),
			},
		},

		{name: "empty email", params: NewUserParams{Email: ""}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "blank email", params: NewUserParams{Email: "   "}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "missing @", params: NewUserParams{Email: "user.example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "missing local part", params: NewUserParams{Email: "@example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "missing domain", params: NewUserParams{Email: "user@"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "missing TLD", params: NewUserParams{Email: "user@example"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "one-letter TLD", params: NewUserParams{Email: "user@example.c"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "two @", params: NewUserParams{Email: "a@b@example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "space inside", params: NewUserParams{Email: "us er@example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "255 characters", params: NewUserParams{Email: strings.Repeat("a", 255-len("@example.com")) + "@example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "non-ASCII letter", params: NewUserParams{Email: "üser@example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "decomposed non-ASCII letter", params: NewUserParams{Email: "üser@example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "non-ASCII domain", params: NewUserParams{Email: "user@exämple.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "fullwidth characters", params: NewUserParams{Email: "ｕｓｅｒ＠example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "compatibility ligature", params: NewUserParams{Email: "ﬁle@example.com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
		{name: "empty domain label", params: NewUserParams{Email: "user@example..com"}, now: testUserCreatedAt, wantErr: apperr.ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewUser(tt.params, tt.now)

			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("NewUser() error = %v, want %v", err, tt.wantErr)
				}
				if got.ID != uuid.Nil() {
					t.Errorf("NewUser() = %+v, want zero value on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewUser() unexpected error: %v", err)
			}

			if got.ID.String() == "00000000-0000-0000-0000-000000000000" {
				t.Error("NewUser() returned a zero ID")
			}
			if diff := cmp.Diff(tt.wantUser, got, cmpopts.IgnoreFields(User{}, "ID")); diff != "" {
				t.Errorf("NewUser() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestNewUser_UniqueIDs(t *testing.T) {
	a, err := NewUser(NewUserParams{Email: "user@example.com"}, testUserCreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewUser(NewUserParams{Email: "user@example.com"}, testUserCreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Errorf("two users share the ID %v", a.ID)
	}
}

func TestChangePassword(t *testing.T) {
	changedAt := utc(testUserNow)

	tests := []struct {
		name     string
		start    User
		hash     string
		wantUser User
	}{
		{
			name:  "sets the hash when there was none",
			start: defaultUser(),
			hash:  "new-hash",
			wantUser: userWith(func(u *User) {
				u.PasswordHash = new("new-hash")
				u.UpdatedAt = changedAt
			}),
		},
		{
			name: "replaces the existing hash",
			start: userWith(func(u *User) {
				u.PasswordHash = new("old-hash")
			}),
			hash: "new-hash",
			wantUser: userWith(func(u *User) {
				u.PasswordHash = new("new-hash")
				u.UpdatedAt = changedAt
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := tt.start
			u.ChangePassword(tt.hash, testUserNow)

			if diff := cmp.Diff(tt.wantUser, u); diff != "" {
				t.Errorf("ChangePassword() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDisable(t *testing.T) {
	later := testUserNow.Add(time.Hour)
	disabledAt := utc(testUserNow)

	tests := []struct {
		name     string
		start    User
		now      time.Time
		wantUser User
	}{
		{
			name:  "enabled user is disabled",
			start: defaultUser(),
			now:   testUserNow,
			wantUser: userWith(func(u *User) {
				u.DisabledAt = &disabledAt
				u.UpdatedAt = disabledAt
			}),
		},
		{
			name: "already disabled user is unchanged",
			start: userWith(func(u *User) {
				u.DisabledAt = &disabledAt
				u.UpdatedAt = disabledAt
			}),
			now: later,
			wantUser: userWith(func(u *User) {
				u.DisabledAt = &disabledAt
				u.UpdatedAt = disabledAt
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := tt.start
			u.Disable(tt.now)

			if diff := cmp.Diff(tt.wantUser, u); diff != "" {
				t.Errorf("Disable() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCanLogin(t *testing.T) {
	verifiedAt := utc(testUserNow)
	disabledAt := utc(testUserNow)

	tests := []struct {
		name    string
		user    User
		wantErr error
	}{
		{
			name: "verified and enabled",
			user: userWith(func(u *User) { u.EmailVerifiedAt = &verifiedAt }),
		},
		{
			name:    "not verified",
			user:    defaultUser(),
			wantErr: ErrEmailNotVerified,
		},
		{
			name: "disabled",
			user: userWith(func(u *User) {
				u.EmailVerifiedAt = &verifiedAt
				u.DisabledAt = &disabledAt
			}),
			wantErr: ErrAccountDisabled,
		},
		{
			// the unverified check comes first
			name:    "not verified and disabled",
			user:    userWith(func(u *User) { u.DisabledAt = &disabledAt }),
			wantErr: ErrEmailNotVerified,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.user.CanLogin()

			if !errors.Is(err, tt.wantErr) {
				t.Errorf("CanLogin() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
