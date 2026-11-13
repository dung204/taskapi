package postgres

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
)

func TestTransactor(t *testing.T) {
	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	errAbort := errors.New("abort")

	newUser := func(t *testing.T) auth.User {
		t.Helper()
		hash := "password-hash"
		u, err := auth.NewUser(auth.NewUserParams{
			Email:        fmt.Sprintf("tx-%s@example.com", uuid.NewV7()),
			PasswordHash: &hash,
		}, time.Now())
		if err != nil {
			t.Fatalf("auth.NewUser: %v", err)
		}
		return u
	}

	t.Run("an error rolls back every write", func(t *testing.T) {
		ctx := t.Context()
		pool := newAuthPool(t, url)
		base := NewAuthStores(sqlcgen.New(pool)) // pool-bound: sees only committed rows
		tx := NewTransactor(pool, NewAuthStores)
		u := newUser(t)

		err := tx.WithTx(ctx, func(s auth.Stores) error {
			if err := s.Users.Create(ctx, u); err != nil {
				return fmt.Errorf("create: %w", err)
			}

			// The write is visible through the transaction's own stores...
			if _, err := s.Users.GetByEmail(ctx, u.Email); err != nil {
				return fmt.Errorf("read inside the transaction: %w", err)
			}
			// ...but not outside it before commit. If the stores handed to fn
			// were bound to the pool, the row would already be committed here.
			_, err := base.Users.GetByEmail(ctx, u.Email)
			if err == nil {
				return errors.New("the row is visible outside the transaction before commit")
			}
			if !errors.Is(err, auth.ErrUserNotFound) {
				return fmt.Errorf("read outside the transaction, want ErrUserNotFound: %w", err)
			}

			return errAbort
		})
		if !errors.Is(err, errAbort) {
			t.Fatalf("WithTx error = %v, want errAbort", err)
		}

		if _, err := base.Users.GetByEmail(ctx, u.Email); !errors.Is(err, auth.ErrUserNotFound) {
			t.Errorf("GetByEmail after rollback = %v, want ErrUserNotFound", err)
		}
	})

	t.Run("success commits", func(t *testing.T) {
		ctx := t.Context()
		pool := newAuthPool(t, url)
		base := NewAuthStores(sqlcgen.New(pool))
		tx := NewTransactor(pool, NewAuthStores)
		u := newUser(t)

		err := tx.WithTx(ctx, func(s auth.Stores) error {
			return s.Users.Create(ctx, u)
		})
		if err != nil {
			t.Fatalf("WithTx: %v", err)
		}

		got, err := base.Users.GetByEmail(ctx, u.Email)
		if err != nil {
			t.Fatalf("GetByEmail after commit: %v", err)
		}
		if got.ID != u.ID {
			t.Errorf("user ID = %v, want %v", got.ID, u.ID)
		}
	})
}
