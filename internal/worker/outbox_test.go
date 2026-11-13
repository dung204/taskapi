package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/pubsub"
)

// waitTimeout bounds every wait for something that should happen.
const waitTimeout = 2 * time.Second

// noTickYet is a poll interval long enough that only NOTIFY can trigger delivery.
const noTickYet = time.Hour

// fakeSubscriber hands out a channel the test controls.
type fakeSubscriber struct {
	events chan pubsub.Event
	err    error

	mu     sync.Mutex
	topic  string
	ctx    context.Context
	called int
}

func newFakeSubscriber() *fakeSubscriber {
	return &fakeSubscriber{events: make(chan pubsub.Event, 8)}
}

func (s *fakeSubscriber) Subscribe(ctx context.Context, topic string) (<-chan pubsub.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.called++
	s.topic, s.ctx = topic, ctx
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

// fakeQueue serves items to Claim once and reports what the worker does with them.
type fakeQueue struct {
	mu      sync.Mutex
	pending []email.ClaimedItem

	claimed chan struct{}
	sent    chan uuid.UUID
}

func newFakeQueue(items ...email.ClaimedItem) *fakeQueue {
	return &fakeQueue{
		pending: items,
		claimed: make(chan struct{}, 64),
		sent:    make(chan uuid.UUID, 64),
	}
}

func (q *fakeQueue) Claim(_ context.Context, _ time.Time, _ time.Duration, _ int32) ([]email.ClaimedItem, error) {
	q.mu.Lock()
	items := q.pending
	q.pending = nil
	q.mu.Unlock()

	select {
	case q.claimed <- struct{}{}:
	default:
	}
	return items, nil
}

func (q *fakeQueue) MarkSent(_ context.Context, id uuid.UUID, _ time.Time) error {
	q.sent <- id
	return nil
}

func (q *fakeQueue) MarkFailed(context.Context, uuid.UUID, time.Time, string) error { return nil }

func (q *fakeQueue) Reschedule(context.Context, uuid.UUID, time.Time, string) error { return nil }

type fakeBuilder struct{}

func (fakeBuilder) Build(_ context.Context, item email.ClaimedItem) (email.Message, error) {
	return email.Message{To: item.ToEmail}, nil
}

type fakeMailer struct{}

func (fakeMailer) Send(context.Context, email.Message) error { return nil }

// startOutbox runs the worker until the test ends and returns a channel that is
// closed when Run returns.
func startOutbox(t *testing.T, sub pubsub.Subscriber, q *fakeQueue, poll time.Duration) (cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()

	w := NewOutboxDelivery(OutboxDeliveryDeps{
		PollInterval: poll,
		Lease:        time.Minute,
		BatchSize:    5,
		SendTimeout:  time.Second,
		MaxAttempts:  3,
		RetryBase:    time.Second,
		RetryCap:     time.Minute,
		Mailer:       fakeMailer{},
		Builder:      fakeBuilder{},
		Outbox:       q,
		Subscriber:   sub,
		Topic:        "email_outbox",
		Logger:       slog.New(slog.DiscardHandler),
	})

	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		w.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(waitTimeout):
			t.Error("Run did not return after its context was cancelled")
		}
	})
	return cancel, finished
}

func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(waitTimeout):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func waitSent(t *testing.T, q *fakeQueue, want uuid.UUID) {
	t.Helper()
	select {
	case got := <-q.sent:
		if got != want {
			t.Errorf("MarkSent(%v), want %v", got, want)
		}
	case <-time.After(waitTimeout):
		t.Fatal("timed out waiting for MarkSent")
	}
}

func TestOutboxDelivery_Notify(t *testing.T) {
	t.Run("an event delivers immediately, without waiting for the ticker", func(t *testing.T) {
		item := email.ClaimedItem{ID: uuid.NewV7(), ToEmail: "alice@example.com", Attempts: 1}
		q := newFakeQueue(item)
		sub := newFakeSubscriber()
		startOutbox(t, sub, q, noTickYet)

		sub.events <- pubsub.Event{Topic: "email_outbox"}

		waitSent(t, q, item.ID)
	})

	t.Run("subscribes to the configured topic with the run context", func(t *testing.T) {
		q := newFakeQueue()
		sub := newFakeSubscriber()
		cancel, done := startOutbox(t, sub, q, noTickYet)

		// An event round trip proves Run got past Subscribe.
		sub.events <- pubsub.Event{}
		waitSignal(t, q.claimed, "Claim")

		sub.mu.Lock()
		topic, ctx, called := sub.topic, sub.ctx, sub.called
		sub.mu.Unlock()

		if topic != "email_outbox" {
			t.Errorf("topic = %q, want email_outbox", topic)
		}
		if called != 1 {
			t.Errorf("Subscribe called %d times, want 1", called)
		}
		if ctx.Err() != nil {
			t.Fatal("subscription context is already cancelled")
		}

		cancel()
		waitSignal(t, done, "Run to return")
		if ctx.Err() == nil {
			t.Error("subscription context must end with the worker")
		}
	})

	t.Run("each event triggers its own claim", func(t *testing.T) {
		q := newFakeQueue()
		sub := newFakeSubscriber()
		startOutbox(t, sub, q, noTickYet)

		for range 3 {
			sub.events <- pubsub.Event{}
			waitSignal(t, q.claimed, "Claim")
		}
	})
}

func TestOutboxDelivery_PollingFallback(t *testing.T) {
	const poll = 10 * time.Millisecond

	tests := []struct {
		name string
		sub  func() pubsub.Subscriber
	}{
		{"no subscriber", func() pubsub.Subscriber { return nil }},
		{"subscribe fails", func() pubsub.Subscriber {
			s := newFakeSubscriber()
			s.err = errors.New("listen failed")
			return s
		}},
		{"subscription closes", func() pubsub.Subscriber {
			s := newFakeSubscriber()
			close(s.events)
			return s
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name+": the ticker still delivers", func(t *testing.T) {
			item := email.ClaimedItem{ID: uuid.NewV7(), ToEmail: "bob@example.com", Attempts: 1}
			q := newFakeQueue(item)
			startOutbox(t, tt.sub(), q, poll)

			waitSent(t, q, item.ID)
		})
	}
}
