package pubsub_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/pubsub"
)

const waitTimeout = 5 * time.Second

var topicSeq atomic.Int64

// newTestSubscriber connects to TEST_DATABASE_URL (the test is skipped when it
// is unset) and returns a subscriber, the pool it uses and a topic no other test
// listens on. LISTEN/NOTIFY needs no schema and leaves no data behind.
func newTestSubscriber(t *testing.T) (*pubsub.PostgresSubscriber, *pgxpool.Pool, string) {
	t.Helper()

	url := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	pool, err := pgxpool.New(t.Context(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	sub := pubsub.NewPostgresSubscriber(pool, slog.New(slog.DiscardHandler))
	// Cleanups run last-in first-out: stop the subscriber, then close the pool,
	// which waits for every connection to be released.
	t.Cleanup(pool.Close)
	t.Cleanup(func() { _ = sub.Close() })

	topic := fmt.Sprintf("pubsub_test_%d_%d", os.Getpid(), topicSeq.Add(1))
	return sub, pool, topic
}

// receive waits for one event on ch, notifying on topic until one arrives: a
// NOTIFY sent before the listener has run LISTEN is lost, and Subscribe gives no
// signal for when LISTEN is in place.
func receive(t *testing.T, pool *pgxpool.Pool, ch <-chan pubsub.Event, topic, payload string) pubsub.Event {
	t.Helper()

	deadline := time.After(waitTimeout)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	for {
		if _, err := pool.Exec(t.Context(), "SELECT pg_notify($1, $2)", topic, payload); err != nil {
			t.Fatalf("notify: %v", err)
		}
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatal("event channel closed while waiting for an event")
			}
			return ev
		case <-tick.C:
		case <-deadline:
			t.Fatal("timed out waiting for an event")
		}
	}
}

func waitClosed(t *testing.T, ch <-chan pubsub.Event, what string) {
	t.Helper()

	timeout := time.After(waitTimeout)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
			// Drain duplicates left over from receive()'s repeated NOTIFYs.
		case <-timeout:
			t.Fatalf("event channel still open: %s", what)
		}
	}
}

func TestPostgresSubscriber_Subscribe(t *testing.T) {
	sub, pool, topic := newTestSubscriber(t)

	ch, err := sub.Subscribe(t.Context(), topic)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	ev := receive(t, pool, ch, topic, "hello")
	if ev.Topic != topic || string(ev.Payload) != "hello" {
		t.Errorf("event = {%q, %q}, want {%q, %q}", ev.Topic, ev.Payload, topic, "hello")
	}
}

func TestPostgresSubscriber_Close(t *testing.T) {
	t.Run("ends every active subscription", func(t *testing.T) {
		sub, pool, topic := newTestSubscriber(t)

		// Two subscriptions on separate topics, both proven live before Close.
		other := topic + "_b"
		a, err := sub.Subscribe(t.Context(), topic)
		if err != nil {
			t.Fatalf("Subscribe a: %v", err)
		}
		b, err := sub.Subscribe(t.Context(), other)
		if err != nil {
			t.Fatalf("Subscribe b: %v", err)
		}
		receive(t, pool, a, topic, "x")
		receive(t, pool, b, other, "x")

		// The context is never cancelled, so only Close can end them.
		if err := sub.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		// Close waits for the subscription goroutines, so the channels are
		// already closed.
		waitClosed(t, a, "after Close")
		waitClosed(t, b, "after Close")
	})

	t.Run("a second Close returns ErrClosed", func(t *testing.T) {
		sub, _, _ := newTestSubscriber(t)

		if err := sub.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if err := sub.Close(); !errors.Is(err, pubsub.ErrClosed) {
			t.Errorf("second Close = %v, want ErrClosed", err)
		}
	})

	t.Run("Subscribe after Close returns ErrClosed", func(t *testing.T) {
		sub, _, topic := newTestSubscriber(t)

		if err := sub.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		ch, err := sub.Subscribe(t.Context(), topic)
		if !errors.Is(err, pubsub.ErrClosed) {
			t.Errorf("Subscribe error = %v, want ErrClosed", err)
		}
		if ch != nil {
			t.Error("Subscribe returned a channel together with an error")
		}
	})

	t.Run("Close with no subscriptions succeeds", func(t *testing.T) {
		sub, _, _ := newTestSubscriber(t)
		if err := sub.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}

func TestPostgresSubscriber_ContextCancel(t *testing.T) {
	sub, pool, topic := newTestSubscriber(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	ch, err := sub.Subscribe(ctx, topic)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	receive(t, pool, ch, topic, "x")

	cancel()
	waitClosed(t, ch, "after its context was cancelled")

	// Cancelling one subscription's context must not close the subscriber.
	if _, err := sub.Subscribe(t.Context(), topic); err != nil {
		t.Errorf("Subscribe after a cancelled subscription: %v", err)
	}
}
