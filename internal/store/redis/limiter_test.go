package redis

import (
	"fmt"
	"os"
	"testing"
	"time"
	"uuid"

	goredis "github.com/redis/go-redis/v9"
)

// newTestClient connects to TEST_REDIS_URL, for example
// redis://localhost:6379/15. The tests use random keys and do not flush the
// database, but it should still not be one you care about. They are skipped
// when the variable is not set.
func newTestClient(t *testing.T) *goredis.Client {
	t.Helper()

	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}

	opts, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse TEST_REDIS_URL: %v", err)
	}

	client := goredis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	if err := client.Ping(t.Context()).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	return client
}

// uniqueEmail keeps tests (and repeated runs) from sharing limiter keys.
func uniqueEmail() string {
	return fmt.Sprintf("%s@example.com", uuid.NewV7())
}

// wantWait checks that a wait returned by a limiter is a real, positive
// duration of the right size: more than half of max and at most max.
func wantWait(t *testing.T, got, max time.Duration) {
	t.Helper()
	if got <= max/2 || got > max {
		t.Errorf("wait = %v, want in (%v, %v]", got, max/2, max)
	}
}

func TestLoginLimiter(t *testing.T) {
	client := newTestClient(t)
	ctx := t.Context()

	const (
		step1 = 600 * time.Millisecond
		step2 = 1200 * time.Millisecond
	)
	newLimiter := func() *LoginLimiter {
		return NewLoginLimiter(client, LoginLimiterOpts{
			FreeAttempts: 2,
			Backoff:      []time.Duration{step1, step2},
			Window:       time.Minute,
		})
	}

	t.Run("free attempts pass without waiting", func(t *testing.T) {
		l, email := newLimiter(), uniqueEmail()
		for i := range 2 {
			wait, err := l.Attempt(ctx, email)
			if err != nil || wait != 0 {
				t.Fatalf("attempt %d = %v, %v, want 0, nil", i+1, wait, err)
			}
		}
	})

	t.Run("the attempt after the free ones is let through and starts the first backoff", func(t *testing.T) {
		l, email := newLimiter(), uniqueEmail()
		for range 2 {
			if _, err := l.Attempt(ctx, email); err != nil {
				t.Fatal(err)
			}
		}

		if wait, err := l.Attempt(ctx, email); err != nil || wait != 0 {
			t.Fatalf("attempt 3 = %v, %v, want 0, nil", wait, err)
		}

		wait, err := l.Attempt(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		wantWait(t, wait, step1)
	})

	t.Run("backoff grows and then stays at the last step", func(t *testing.T) {
		l, email := newLimiter(), uniqueEmail()
		for range 3 { // 2 free + the one that starts the first lock
			if _, err := l.Attempt(ctx, email); err != nil {
				t.Fatal(err)
			}
		}

		time.Sleep(step1 + 100*time.Millisecond)
		if wait, err := l.Attempt(ctx, email); err != nil || wait != 0 {
			t.Fatalf("attempt after the first lock = %v, %v, want 0, nil", wait, err)
		}
		wait, err := l.Attempt(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		wantWait(t, wait, step2)

		time.Sleep(step2 + 100*time.Millisecond)
		if wait, err := l.Attempt(ctx, email); err != nil || wait != 0 {
			t.Fatalf("attempt after the second lock = %v, %v, want 0, nil", wait, err)
		}
		wait, err = l.Attempt(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		wantWait(t, wait, step2) // clamped to the final step
	})

	t.Run("Reset clears the counter and the lock", func(t *testing.T) {
		l, email := newLimiter(), uniqueEmail()
		for range 4 { // the 4th is locked out
			if _, err := l.Attempt(ctx, email); err != nil {
				t.Fatal(err)
			}
		}

		if err := l.Reset(ctx, email); err != nil {
			t.Fatalf("Reset() = %v", err)
		}
		for i := range 2 {
			if wait, err := l.Attempt(ctx, email); err != nil || wait != 0 {
				t.Fatalf("attempt %d after reset = %v, %v, want 0, nil", i+1, wait, err)
			}
		}
	})

	t.Run("emails are limited independently", func(t *testing.T) {
		l, a, b := newLimiter(), uniqueEmail(), uniqueEmail()
		for range 4 {
			if _, err := l.Attempt(ctx, a); err != nil {
				t.Fatal(err)
			}
		}
		if wait, err := l.Attempt(ctx, b); err != nil || wait != 0 {
			t.Errorf("other email = %v, %v, want 0, nil", wait, err)
		}
	})
}

func TestMailRequestLimiter(t *testing.T) {
	client := newTestClient(t)
	ctx := t.Context()

	const cooldown = 500 * time.Millisecond
	newLimiter := func(hourlyMax int) *AuthMailLimiter {
		return NewAuthMailLimiter(client, AuthMailLimiterOpts{
			RequestCooldown:  cooldown,
			RequestHourlyMax: hourlyMax,
		})
	}

	t.Run("the first request passes, the next one waits for the cooldown", func(t *testing.T) {
		l, email := newLimiter(10), uniqueEmail()

		if wait, err := l.Allow(ctx, email); err != nil || wait != 0 {
			t.Fatalf("first request = %v, %v, want 0, nil", wait, err)
		}

		wait, err := l.Allow(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		wantWait(t, wait, cooldown)
	})

	t.Run("a request after the cooldown passes again", func(t *testing.T) {
		l, email := newLimiter(10), uniqueEmail()
		if _, err := l.Allow(ctx, email); err != nil {
			t.Fatal(err)
		}

		time.Sleep(cooldown + 100*time.Millisecond)
		if wait, err := l.Allow(ctx, email); err != nil || wait != 0 {
			t.Errorf("request after the cooldown = %v, %v, want 0, nil", wait, err)
		}
	})

	t.Run("the hourly cap refuses with a wait of up to an hour", func(t *testing.T) {
		l, email := newLimiter(2), uniqueEmail()
		for i := range 2 {
			if wait, err := l.Allow(ctx, email); err != nil || wait != 0 {
				t.Fatalf("request %d = %v, %v, want 0, nil", i+1, wait, err)
			}
			time.Sleep(cooldown + 100*time.Millisecond)
		}

		wait, err := l.Allow(ctx, email)
		if err != nil {
			t.Fatal(err)
		}
		wantWait(t, wait, authMailWindow)
	})

	t.Run("emails are limited independently", func(t *testing.T) {
		l, a, b := newLimiter(10), uniqueEmail(), uniqueEmail()
		if _, err := l.Allow(ctx, a); err != nil {
			t.Fatal(err)
		}
		if wait, err := l.Allow(ctx, b); err != nil || wait != 0 {
			t.Errorf("other email = %v, %v, want 0, nil", wait, err)
		}
	})
}

func TestHashEmail(t *testing.T) {
	a, b := hashEmail("alice@example.com"), hashEmail("bob@example.com")
	if a == b {
		t.Error("different emails hash to the same key")
	}
	if a != hashEmail("alice@example.com") {
		t.Error("hashEmail is not deterministic")
	}
	if got := hashEmail("alice@example.com"); got == "alice@example.com" {
		t.Error("hashEmail returns the address itself")
	}
}
