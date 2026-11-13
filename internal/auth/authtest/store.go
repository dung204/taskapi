// Package authtest holds test helpers shared by the auth store implementations,
// including the behaviour suites that every [auth.SessionStore],
// [auth.UserStore] and [auth.UserTokenStore] must pass.
package authtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/auth"
)

var base = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// The lifetimes the seed helpers give to the sessions and tokens they create.
// They only need to outlive the moments the tests use them at (a minute or so
// after base); tests of expiry set the time themselves.
const (
	sessionTTL = 30 * 24 * time.Hour
	tokenTTL   = 24 * time.Hour
)

// hashOf returns a 32-byte hash filled with b, so tests can tell hashes apart.
func hashOf(b byte) []byte {
	return bytes.Repeat([]byte{b}, 32)
}

// SeedUser creates a user with a unique email through the store. Other test
// packages use it to satisfy foreign keys to users.
func SeedUser(ctx context.Context, t *testing.T, us auth.UserStore) auth.User {
	t.Helper()

	hash := "password-hash"
	u, err := auth.NewUser(auth.NewUserParams{
		Email:        fmt.Sprintf("user-%s@example.com", uuid.NewV7()),
		PasswordHash: &hash,
	}, base)
	if err != nil {
		t.Fatalf("auth.NewUser: %v", err)
	}

	if err := us.Create(ctx, u); err != nil {
		t.Fatalf("Create user: %v", err)
	}

	return u
}

func TestUserStore(t *testing.T, newStore func(t *testing.T) auth.UserStore) {
	ctx := context.Background()

	newUser := func(t *testing.T, email string, hash *string) auth.User {
		t.Helper()

		u, err := auth.NewUser(auth.NewUserParams{Email: email, PasswordHash: hash}, base)
		if err != nil {
			t.Fatalf("auth.NewUser: %v", err)
		}
		return u
	}

	t.Run("Create then get by id and by email", func(t *testing.T) {
		hash := "hash"
		tests := []struct {
			name string
			hash *string
		}{
			{"with password", &hash},
			{"without password", nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s := newStore(t)
				u := newUser(t, "alice@example.com", tt.hash)

				if err := s.Create(ctx, u); err != nil {
					t.Fatalf("Create: %v", err)
				}

				got, err := s.GetByID(ctx, u.ID)
				if err != nil {
					t.Fatalf("GetByID: %v", err)
				}
				CheckUser(t, got, u)

				got, err = s.GetByEmail(ctx, u.Email)
				if err != nil {
					t.Fatalf("GetByEmail: %v", err)
				}
				CheckUser(t, got, u)
			})
		}
	})

	t.Run("Create returns ErrEmailTaken for a duplicate email", func(t *testing.T) {
		s := newStore(t)
		first := newUser(t, "alice@example.com", nil)
		if err := s.Create(ctx, first); err != nil {
			t.Fatalf("Create: %v", err)
		}

		err := s.Create(ctx, newUser(t, "alice@example.com", nil))
		if !errors.Is(err, auth.ErrEmailTaken) {
			t.Errorf("err = %v, want %v", err, auth.ErrEmailTaken)
		}

		// The first user is untouched.
		got, err := s.GetByEmail(ctx, first.Email)
		if err != nil {
			t.Fatalf("GetByEmail: %v", err)
		}
		CheckUser(t, got, first)
	})

	t.Run("GetByID returns ErrUserNotFound for unknown id", func(t *testing.T) {
		s := newStore(t)

		_, err := s.GetByID(ctx, uuid.NewV7())
		if !errors.Is(err, auth.ErrUserNotFound) {
			t.Errorf("err = %v, want %v", err, auth.ErrUserNotFound)
		}
	})

	t.Run("GetByEmail returns ErrUserNotFound for unknown email", func(t *testing.T) {
		s := newStore(t)

		_, err := s.GetByEmail(ctx, "nobody@example.com")
		if !errors.Is(err, auth.ErrUserNotFound) {
			t.Errorf("err = %v, want %v", err, auth.ErrUserNotFound)
		}
	})

	t.Run("SetEmailVerified updates email_verified_at and updated_at", func(t *testing.T) {
		s := newStore(t)
		u := newUser(t, "alice@example.com", nil)
		if err := s.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}

		at := base.Add(time.Hour)
		if err := s.SetEmailVerified(ctx, u.ID, at); err != nil {
			t.Fatalf("SetEmailVerified: %v", err)
		}

		got, err := s.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}

		want := u
		want.EmailVerifiedAt = &at
		want.UpdatedAt = at
		CheckUser(t, got, want)
	})

	t.Run("SetEmailVerified returns ErrUserNotFound for unknown id", func(t *testing.T) {
		s := newStore(t)

		err := s.SetEmailVerified(ctx, uuid.NewV7(), base)
		if !errors.Is(err, auth.ErrUserNotFound) {
			t.Errorf("err = %v, want %v", err, auth.ErrUserNotFound)
		}
	})

	t.Run("UpdatePassword updates password_hash, updated_at and tokens_valid_after", func(t *testing.T) {
		old := "old-hash"
		tests := []struct {
			name string
			hash *string
		}{
			{"replaces a password", &old},
			{"sets a first password", nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s := newStore(t)
				u := newUser(t, "alice@example.com", tt.hash)
				if err := s.Create(ctx, u); err != nil {
					t.Fatalf("Create: %v", err)
				}

				at := base.Add(time.Hour)
				if err := s.UpdatePassword(ctx, u.ID, "new-hash", at); err != nil {
					t.Fatalf("UpdatePassword: %v", err)
				}

				got, err := s.GetByID(ctx, u.ID)
				if err != nil {
					t.Fatalf("GetByID: %v", err)
				}

				want := u
				want.PasswordHash = new("new-hash")
				want.UpdatedAt = at
				// A password change revokes every token issued before it.
				want.TokensValidAfter = &at
				CheckUser(t, got, want)
			})
		}
	})

	t.Run("UpdatePassword returns ErrUserNotFound for unknown id", func(t *testing.T) {
		s := newStore(t)

		err := s.UpdatePassword(ctx, uuid.NewV7(), "hash", base)
		if !errors.Is(err, auth.ErrUserNotFound) {
			t.Errorf("err = %v, want %v", err, auth.ErrUserNotFound)
		}
	})
}

// TestSessionStore runs the session behaviour suite. newStore also returns a
// [auth.UserStore] over the same data, because sessions belong to users and a
// real backend rejects a session whose user does not exist.
func TestSessionStore(t *testing.T, newStore func(t *testing.T) (auth.SessionStore, auth.UserStore)) {
	ctx := context.Background()

	// seed creates a session through the store for the given user.
	seed := func(t *testing.T, s auth.SessionStore, userID uuid.UUID, hash []byte, ua *string) auth.Session {
		t.Helper()

		ss, err := auth.NewSession(auth.NewSessionParams{
			UserID: userID, RefreshTokenHash: hash, UserAgent: ua, TTL: sessionTTL,
		}, base)
		if err != nil {
			t.Fatalf("auth.NewSession: %v", err)
		}

		if err := s.Create(ctx, ss); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return ss
	}

	// seedExpiring creates a session that expires at the given time.
	seedExpiring := func(t *testing.T, s auth.SessionStore, userID uuid.UUID, hash []byte, expiresAt time.Time) auth.Session {
		t.Helper()

		ss, err := auth.NewSession(auth.NewSessionParams{UserID: userID, RefreshTokenHash: hash}, base)
		if err != nil {
			t.Fatalf("auth.NewSession: %v", err)
		}
		ss.ExpiresAt = expiresAt

		if err := s.Create(ctx, ss); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return ss
	}

	mustGet := func(t *testing.T, s auth.SessionStore, hash []byte) auth.Session {
		t.Helper()

		got, err := s.GetByRefreshTokenHash(ctx, hash)
		if err != nil {
			t.Fatalf("GetByRefreshTokenHash: %v", err)
		}
		return got
	}

	checkGone := func(t *testing.T, s auth.SessionStore, hash []byte) {
		t.Helper()

		_, err := s.GetByRefreshTokenHash(ctx, hash)
		if !errors.Is(err, auth.ErrSessionNotFound) {
			t.Errorf("GetByRefreshTokenHash: err = %v, want %v", err, auth.ErrSessionNotFound)
		}
	}

	t.Run("Create then get by refresh token hash", func(t *testing.T) {
		ua := "Mozilla/5.0"
		tests := []struct {
			name string
			ua   *string
		}{
			{"with user agent", &ua},
			{"without user agent", nil},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s, us := newStore(t)
				u := SeedUser(ctx, t, us)
				ss := seed(t, s, u.ID, hashOf(1), tt.ua)

				CheckSession(t, mustGet(t, s, hashOf(1)), ss)
			})
		}
	})

	t.Run("Create rejects a duplicate refresh token hash", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		first := seed(t, s, u.ID, hashOf(1), nil)

		dup, err := auth.NewSession(auth.NewSessionParams{UserID: u.ID, RefreshTokenHash: hashOf(1)}, base)
		if err != nil {
			t.Fatalf("auth.NewSession: %v", err)
		}
		if err := s.Create(ctx, dup); err == nil {
			t.Error("Create: err = nil, want an error")
		}

		CheckSession(t, mustGet(t, s, hashOf(1)), first)
	})

	t.Run("GetByRefreshTokenHash returns ErrSessionNotFound for unknown hash", func(t *testing.T) {
		s, _ := newStore(t)

		checkGone(t, s, hashOf(9))
	})

	t.Run("Rotate swaps the hash and keeps the old one as previous", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		ss := seed(t, s, u.ID, hashOf(1), nil)

		now := base.Add(time.Minute)
		if err := s.Rotate(ctx, ss.ID, hashOf(1), hashOf(2), now); err != nil {
			t.Fatalf("Rotate: %v", err)
		}

		want := ss
		want.RefreshTokenHash = hashOf(2)
		want.PreviousRefreshTokenHash = hashOf(1)
		want.RotatedAt = &now

		// Both the new and the previous token find the same session.
		CheckSession(t, mustGet(t, s, hashOf(2)), want)
		CheckSession(t, mustGet(t, s, hashOf(1)), want)
	})

	t.Run("Rotate can be repeated with the current hash", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		ss := seed(t, s, u.ID, hashOf(1), nil)

		if err := s.Rotate(ctx, ss.ID, hashOf(1), hashOf(2), base.Add(time.Minute)); err != nil {
			t.Fatalf("first Rotate: %v", err)
		}
		second := base.Add(2 * time.Minute)
		if err := s.Rotate(ctx, ss.ID, hashOf(2), hashOf(3), second); err != nil {
			t.Fatalf("second Rotate: %v", err)
		}

		want := ss
		want.RefreshTokenHash = hashOf(3)
		want.PreviousRefreshTokenHash = hashOf(2)
		want.RotatedAt = &second
		CheckSession(t, mustGet(t, s, hashOf(3)), want)
		checkGone(t, s, hashOf(1)) // two rotations ago: no longer known
	})

	t.Run("Rotate returns ErrSessionNotRotated", func(t *testing.T) {
		tests := []struct {
			name string
			// call receives the seeded session and runs the Rotate under test.
			call func(s auth.SessionStore, ss auth.Session) error
		}{
			{"unknown session id", func(s auth.SessionStore, ss auth.Session) error {
				return s.Rotate(ctx, uuid.NewV7(), hashOf(1), hashOf(2), base.Add(time.Minute))
			}},
			{"wrong old hash", func(s auth.SessionStore, ss auth.Session) error {
				return s.Rotate(ctx, ss.ID, hashOf(8), hashOf(2), base.Add(time.Minute))
			}},
			{"old hash is a previous hash", func(s auth.SessionStore, ss auth.Session) error {
				if err := s.Rotate(ctx, ss.ID, hashOf(1), hashOf(2), base.Add(time.Minute)); err != nil {
					return fmt.Errorf("setup Rotate: %w", err)
				}
				return s.Rotate(ctx, ss.ID, hashOf(1), hashOf(3), base.Add(2*time.Minute))
			}},
			{"session expired exactly now", func(s auth.SessionStore, ss auth.Session) error {
				return s.Rotate(ctx, ss.ID, hashOf(1), hashOf(2), ss.ExpiresAt)
			}},
			{"session expired", func(s auth.SessionStore, ss auth.Session) error {
				return s.Rotate(ctx, ss.ID, hashOf(1), hashOf(2), ss.ExpiresAt.Add(time.Hour))
			}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s, us := newStore(t)
				u := SeedUser(ctx, t, us)
				ss := seed(t, s, u.ID, hashOf(1), nil)

				err := tt.call(s, ss)
				if !errors.Is(err, auth.ErrSessionNotRotated) {
					t.Errorf("err = %v, want %v", err, auth.ErrSessionNotRotated)
				}
			})
		}
	})

	t.Run("failed Rotate leaves the session unchanged", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		ss := seed(t, s, u.ID, hashOf(1), nil)

		if err := s.Rotate(ctx, ss.ID, hashOf(8), hashOf(2), base.Add(time.Minute)); !errors.Is(err, auth.ErrSessionNotRotated) {
			t.Fatalf("Rotate: err = %v, want %v", err, auth.ErrSessionNotRotated)
		}

		CheckSession(t, mustGet(t, s, hashOf(1)), ss)
		checkGone(t, s, hashOf(2))
	})

	t.Run("DeleteByID removes only that session", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		doomed := seed(t, s, u.ID, hashOf(1), nil)
		kept := seed(t, s, u.ID, hashOf(2), nil)

		if err := s.DeleteByID(ctx, doomed.ID); err != nil {
			t.Fatalf("DeleteByID: %v", err)
		}

		checkGone(t, s, hashOf(1))
		CheckSession(t, mustGet(t, s, hashOf(2)), kept)
	})

	t.Run("DeleteByID of an unknown id is not an error", func(t *testing.T) {
		s, _ := newStore(t)

		if err := s.DeleteByID(ctx, uuid.NewV7()); err != nil {
			t.Errorf("DeleteByID: %v", err)
		}
	})

	t.Run("DeleteByUserID removes only that user's sessions", func(t *testing.T) {
		s, us := newStore(t)
		alice, bob := SeedUser(ctx, t, us), SeedUser(ctx, t, us)
		seed(t, s, alice.ID, hashOf(1), nil)
		seed(t, s, alice.ID, hashOf(2), nil)
		kept := seed(t, s, bob.ID, hashOf(3), nil)

		if err := s.DeleteByUserID(ctx, alice.ID); err != nil {
			t.Fatalf("DeleteByUserID: %v", err)
		}

		checkGone(t, s, hashOf(1))
		checkGone(t, s, hashOf(2))
		CheckSession(t, mustGet(t, s, hashOf(3)), kept)

		// A user without sessions is fine.
		if err := s.DeleteByUserID(ctx, alice.ID); err != nil {
			t.Errorf("second DeleteByUserID: %v", err)
		}
	})

	t.Run("DeleteExpired removes sessions expired at or before now", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		seedExpiring(t, s, u.ID, hashOf(1), base.Add(-time.Hour))
		seedExpiring(t, s, u.ID, hashOf(2), base) // expires exactly now: expired
		kept := seedExpiring(t, s, u.ID, hashOf(3), base.Add(time.Hour))

		n, err := s.DeleteExpired(ctx, base, 100)
		if err != nil {
			t.Fatalf("DeleteExpired: %v", err)
		}
		if n != 2 {
			t.Errorf("count = %d, want 2", n)
		}

		checkGone(t, s, hashOf(1))
		checkGone(t, s, hashOf(2))
		CheckSession(t, mustGet(t, s, hashOf(3)), kept)

		n, err = s.DeleteExpired(ctx, base, 100)
		if err != nil {
			t.Fatalf("second DeleteExpired: %v", err)
		}
		if n != 0 {
			t.Errorf("second count = %d, want 0", n)
		}
	})

	t.Run("DeleteExpired deletes at most limit sessions", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		for i := range 3 {
			seedExpiring(t, s, u.ID, hashOf(byte(i+1)), base.Add(-time.Hour))
		}

		for _, want := range []int64{2, 1, 0} {
			n, err := s.DeleteExpired(ctx, base, 2)
			if err != nil {
				t.Fatalf("DeleteExpired: %v", err)
			}
			if n != want {
				t.Errorf("count = %d, want %d", n, want)
			}
		}
	})
}

// TestUserTokenStore runs the one-time user token behaviour suite. newStore
// also returns a [auth.UserStore] over the same data, because tokens belong to
// users and a real backend rejects a token whose user does not exist.
func TestUserTokenStore(t *testing.T, newStore func(t *testing.T) (auth.UserTokenStore, auth.UserStore)) {
	ctx := context.Background()

	// seed creates a token through the store that is still valid when the tests
	// consume it (a minute after base).
	seed := func(t *testing.T, s auth.UserTokenStore, userID uuid.UUID, purpose auth.TokenPurpose, hash []byte) auth.UserToken {
		t.Helper()

		tok, err := auth.NewUserToken(auth.NewUserTokenParams{
			UserID: userID, Purpose: purpose, TokenHash: hash, TTL: tokenTTL,
		}, base)
		if err != nil {
			t.Fatalf("auth.NewUserToken: %v", err)
		}

		if err := s.Create(ctx, tok); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return tok
	}

	// seedExpiring creates a token that expires at the given time.
	seedExpiring := func(t *testing.T, s auth.UserTokenStore, userID uuid.UUID, hash []byte, expiresAt time.Time) {
		t.Helper()

		tok, err := auth.NewUserToken(auth.NewUserTokenParams{
			UserID: userID, Purpose: auth.PurposeVerifyEmail, TokenHash: hash,
		}, base)
		if err != nil {
			t.Fatalf("auth.NewUserToken: %v", err)
		}
		tok.ExpiresAt = expiresAt

		if err := s.Create(ctx, tok); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	checkConsumable := func(t *testing.T, s auth.UserTokenStore, hash []byte, purpose auth.TokenPurpose, want uuid.UUID) {
		t.Helper()

		got, err := s.Consume(ctx, hash, purpose, base.Add(time.Minute))
		if err != nil {
			t.Fatalf("Consume: %v", err)
		}
		if got != want {
			t.Errorf("user id = %v, want %v", got, want)
		}
	}

	checkInvalid := func(t *testing.T, s auth.UserTokenStore, hash []byte, purpose auth.TokenPurpose) {
		t.Helper()

		_, err := s.Consume(ctx, hash, purpose, base.Add(time.Minute))
		if !errors.Is(err, auth.ErrUserTokenInvalid) {
			t.Errorf("Consume: err = %v, want %v", err, auth.ErrUserTokenInvalid)
		}
	}

	t.Run("Consume returns the user id of a valid token", func(t *testing.T) {
		for _, purpose := range []auth.TokenPurpose{auth.PurposeVerifyEmail, auth.PurposeResetPassword} {
			t.Run(string(purpose), func(t *testing.T) {
				s, us := newStore(t)
				u := SeedUser(ctx, t, us)
				seed(t, s, u.ID, purpose, hashOf(1))

				checkConsumable(t, s, hashOf(1), purpose, u.ID)
			})
		}
	})

	t.Run("Consume is single use", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		seed(t, s, u.ID, auth.PurposeVerifyEmail, hashOf(1))

		checkConsumable(t, s, hashOf(1), auth.PurposeVerifyEmail, u.ID)
		checkInvalid(t, s, hashOf(1), auth.PurposeVerifyEmail)
	})

	t.Run("Consume returns ErrUserTokenInvalid", func(t *testing.T) {
		tests := []struct {
			name    string
			hash    []byte
			purpose auth.TokenPurpose
			now     time.Time
		}{
			{"unknown hash", hashOf(9), auth.PurposeVerifyEmail, base.Add(time.Minute)},
			{"wrong purpose", hashOf(1), auth.PurposeResetPassword, base.Add(time.Minute)},
			{"expired", hashOf(1), auth.PurposeVerifyEmail, base.Add(48 * time.Hour)},
			{"expired exactly now", hashOf(1), auth.PurposeVerifyEmail, base.Add(24 * time.Hour)},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				s, us := newStore(t)
				u := SeedUser(ctx, t, us)
				seed(t, s, u.ID, auth.PurposeVerifyEmail, hashOf(1))

				_, err := s.Consume(ctx, tt.hash, tt.purpose, tt.now)
				if !errors.Is(err, auth.ErrUserTokenInvalid) {
					t.Errorf("err = %v, want %v", err, auth.ErrUserTokenInvalid)
				}
			})
		}
	})

	t.Run("a failed Consume does not use up the token", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		seed(t, s, u.ID, auth.PurposeVerifyEmail, hashOf(1))

		checkInvalid(t, s, hashOf(1), auth.PurposeResetPassword)
		checkConsumable(t, s, hashOf(1), auth.PurposeVerifyEmail, u.ID)
	})

	t.Run("Create rejects a duplicate token hash", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		seed(t, s, u.ID, auth.PurposeVerifyEmail, hashOf(1))

		dup, err := auth.NewUserToken(auth.NewUserTokenParams{
			UserID: u.ID, Purpose: auth.PurposeResetPassword, TokenHash: hashOf(1),
		}, base)
		if err != nil {
			t.Fatalf("auth.NewUserToken: %v", err)
		}
		if err := s.Create(ctx, dup); err == nil {
			t.Error("Create: err = nil, want an error")
		}
	})

	t.Run("DeleteByUserAndPurpose removes only that user's tokens of that purpose", func(t *testing.T) {
		s, us := newStore(t)
		alice, bob := SeedUser(ctx, t, us), SeedUser(ctx, t, us)
		seed(t, s, alice.ID, auth.PurposeVerifyEmail, hashOf(1))
		seed(t, s, alice.ID, auth.PurposeVerifyEmail, hashOf(2))
		seed(t, s, alice.ID, auth.PurposeResetPassword, hashOf(3))
		seed(t, s, bob.ID, auth.PurposeVerifyEmail, hashOf(4))

		if err := s.DeleteByUserAndPurpose(ctx, alice.ID, auth.PurposeVerifyEmail); err != nil {
			t.Fatalf("DeleteByUserAndPurpose: %v", err)
		}

		checkInvalid(t, s, hashOf(1), auth.PurposeVerifyEmail)
		checkInvalid(t, s, hashOf(2), auth.PurposeVerifyEmail)
		checkConsumable(t, s, hashOf(3), auth.PurposeResetPassword, alice.ID)
		checkConsumable(t, s, hashOf(4), auth.PurposeVerifyEmail, bob.ID)

		// Nothing left to delete is fine.
		if err := s.DeleteByUserAndPurpose(ctx, alice.ID, auth.PurposeVerifyEmail); err != nil {
			t.Errorf("second DeleteByUserAndPurpose: %v", err)
		}
	})

	t.Run("DeleteExpired removes tokens expired at or before now", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		seedExpiring(t, s, u.ID, hashOf(1), base.Add(-time.Hour))
		seedExpiring(t, s, u.ID, hashOf(2), base) // expires exactly now: expired
		seedExpiring(t, s, u.ID, hashOf(3), base.Add(time.Hour))

		n, err := s.DeleteExpired(ctx, base, 100)
		if err != nil {
			t.Fatalf("DeleteExpired: %v", err)
		}
		if n != 2 {
			t.Errorf("count = %d, want 2", n)
		}

		checkConsumable(t, s, hashOf(3), auth.PurposeVerifyEmail, u.ID)

		n, err = s.DeleteExpired(ctx, base, 100)
		if err != nil {
			t.Fatalf("second DeleteExpired: %v", err)
		}
		if n != 0 {
			t.Errorf("second count = %d, want 0", n)
		}
	})

	t.Run("DeleteExpired also removes used tokens once they expire", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		seed(t, s, u.ID, auth.PurposeResetPassword, hashOf(1))
		checkConsumable(t, s, hashOf(1), auth.PurposeResetPassword, u.ID)

		n, err := s.DeleteExpired(ctx, base.Add(24*time.Hour), 100)
		if err != nil {
			t.Fatalf("DeleteExpired: %v", err)
		}
		if n != 1 {
			t.Errorf("count = %d, want 1", n)
		}
	})

	t.Run("DeleteExpired deletes at most limit tokens", func(t *testing.T) {
		s, us := newStore(t)
		u := SeedUser(ctx, t, us)
		for i := range 3 {
			seedExpiring(t, s, u.ID, hashOf(byte(i+1)), base.Add(-time.Hour))
		}

		for _, want := range []int64{2, 1, 0} {
			n, err := s.DeleteExpired(ctx, base, 2)
			if err != nil {
				t.Fatalf("DeleteExpired: %v", err)
			}
			if n != want {
				t.Errorf("count = %d, want %d", n, want)
			}
		}
	})
}
