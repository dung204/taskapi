package pubsub

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresSubscriber struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	mu     sync.Mutex
	closed bool
	base   context.Context // cancelled by Close; ends every subscription
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

var _ Subscriber = (*PostgresSubscriber)(nil)

func NewPostgresSubscriber(pool *pgxpool.Pool, logger *slog.Logger) *PostgresSubscriber {
	base, cancel := context.WithCancel(context.Background())
	return &PostgresSubscriber{
		pool:   pool,
		logger: logger,
		base:   base,
		cancel: cancel,
	}
}

// Subscribe implements [Subscriber].
func (s *PostgresSubscriber) Subscribe(ctx context.Context, topic string) (<-chan Event, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}

	s.wg.Add(1)
	s.mu.Unlock()

	out := make(chan Event, 64)

	subCtx, subCancel := context.WithCancel(ctx)
	// Close cancels s.base, which must also end this subscription.
	//nolint:contextcheck // s.base is the subscriber's own lifetime, not the caller's context
	stopAfter := context.AfterFunc(s.base, subCancel)

	go func() {
		defer s.wg.Done()
		defer subCancel()
		defer stopAfter()
		defer close(out)

		s.runListenLoop(subCtx, topic, out)
	}()

	return out, nil
}

func (s *PostgresSubscriber) runListenLoop(ctx context.Context, topic string, out chan<- Event) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := s.listenSession(ctx, topic, out); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}

			s.logger.WarnContext(ctx, "postgres LISTEN connection is lost, reconnecting after 2 seconds",
				"topic", topic,
				"error", err,
			)

			select {
			case <-time.After(2 * time.Second):
			case <-ctx.Done():
				return
			}
		}
	}
}

func (s *PostgresSubscriber) listenSession(ctx context.Context, topic string, out chan<- Event) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	sanitizedTopic := pgx.Identifier{topic}.Sanitize()

	if _, err = conn.Exec(ctx, "LISTEN "+sanitizedTopic); err != nil {
		return err
	}
	s.logger.InfoContext(ctx, "established LISTEN connection successfully", "topic", topic)

	for {
		noti, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err // network error, timeout, context.Canceled
		}

		s.logger.DebugContext(ctx, "received notification from Postgres",
			"channel", noti.Channel,
			"payload", noti.Payload,
		)

		event := Event{
			Topic:   noti.Channel,
			Payload: []byte(noti.Payload),
		}

		select {
		case out <- event:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *PostgresSubscriber) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	s.closed = true
	s.mu.Unlock()

	s.cancel()
	s.wg.Wait()
	return nil
}
