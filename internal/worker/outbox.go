package worker

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/pubsub"
)

// Mailer delivers emails. It is satisfied by [email.Mailer].
type Mailer interface {
	// Send delivers an [email.Message] to its recipient.
	//
	// Returns the following errors:
	//   - an error - if the message is invalid or could not be delivered; the
	//     worker then retries the item later or gives up on it.
	Send(ctx context.Context, msg email.Message) error
}

// OutboxQueue is the part of [email.OutboxStore] the delivery worker uses; see
// it for the full behavior of each method.
type OutboxQueue interface {
	// Claim leases up to limit due items to the worker until now+lease.
	//
	// Returns:
	//   - items - the claimed [email.ClaimedItem]s.
	Claim(ctx context.Context, now time.Time, lease time.Duration, limit int32) (items []email.ClaimedItem, err error)

	// MarkSent records that the item was delivered at at.
	MarkSent(ctx context.Context, id uuid.UUID, at time.Time) error

	// MarkFailed gives up on the item for good, recording lastError.
	MarkFailed(ctx context.Context, id uuid.UUID, at time.Time, lastError string) error

	// Reschedule makes the item claimable again from nextAttemptAt, recording
	// lastError.
	Reschedule(ctx context.Context, id uuid.UUID, nextAttemptAt time.Time, lastError string) error
}

type OutboxDelivery struct {
	pollInterval time.Duration

	lease       time.Duration
	batchSize   int
	sendTimeout time.Duration

	maxAttempts int
	retryBase   time.Duration
	retryCap    time.Duration

	mailer  Mailer
	builder email.MessageBuilder
	outbox  OutboxQueue
	sub     pubsub.Subscriber
	topic   string
	logger  *slog.Logger
	now     func() time.Time
}

type OutboxDeliveryDeps struct {
	PollInterval time.Duration

	Lease       time.Duration
	BatchSize   int
	SendTimeout time.Duration

	MaxAttempts int
	RetryBase   time.Duration
	RetryCap    time.Duration

	Mailer  Mailer
	Builder email.MessageBuilder
	Outbox  OutboxQueue
	// Subscriber and Topic deliver wake-up events (the email_outbox NOTIFY).
	// Subscriber may be nil, in which case only the poll ticker triggers delivery.
	Subscriber pubsub.Subscriber
	Topic      string
	Logger     *slog.Logger
	Now        func() time.Time
}

func NewOutboxDelivery(deps OutboxDeliveryDeps) *OutboxDelivery {
	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &OutboxDelivery{
		deps.PollInterval,
		deps.Lease,
		deps.BatchSize,
		deps.SendTimeout,
		deps.MaxAttempts,
		deps.RetryBase,
		deps.RetryCap,
		deps.Mailer,
		deps.Builder,
		deps.Outbox,
		deps.Subscriber,
		deps.Topic,
		deps.Logger,
		deps.Now,
	}
}

var _ Runner = (*OutboxDelivery)(nil)

func (r *OutboxDelivery) Run(ctx context.Context) {
	fallbackTicker := time.NewTicker(r.pollInterval)
	defer fallbackTicker.Stop()

	// A nil channel blocks forever in select, so without a subscription (none
	// configured, subscribe failed, or the subscription ended) the worker falls
	// back to the ticker alone.
	var events <-chan pubsub.Event
	if r.sub != nil {
		var err error
		events, err = r.sub.Subscribe(ctx, r.topic)
		if err != nil {
			r.logger.ErrorContext(ctx, "outbox subscribe failed, relying on polling", "error", err)
			events = nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			r.logger.InfoContext(ctx, "outbox delivery worker stopped")
			return

		case _, ok := <-events:
			if !ok {
				r.logger.WarnContext(ctx, "outbox subscription ended, relying on polling")
				events = nil
				continue
			}
			r.logger.DebugContext(ctx, "outbox delivery worker triggered by NOTIFY event")
			r.deliverOutbox(ctx, r.now())

		case t := <-fallbackTicker.C:
			r.logger.DebugContext(ctx, "outbox delivery worker triggered by fallback ticker")
			r.deliverOutbox(ctx, t)
		}
	}
}

func (r *OutboxDelivery) deliverOutbox(ctx context.Context, t time.Time) {
	jobs, err := r.outbox.Claim(ctx, t, r.lease, int32(r.batchSize))
	if err != nil {
		r.logger.ErrorContext(ctx, "deliver outbox failed", "error", err)
		return
	}

	for _, job := range jobs {
		r.deliver(ctx, job)
	}
}

func (r *OutboxDelivery) deliver(ctx context.Context, job email.ClaimedItem) {
	msg, err := r.builder.Build(ctx, job)
	switch {
	case errors.Is(err, email.ErrNothingToSend):
		r.markDone(ctx, job)
		return
	case err != nil:
		r.fail(ctx, job, err)
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, r.sendTimeout)
	err = r.mailer.Send(sendCtx, msg)
	cancel()
	if err != nil {
		r.fail(ctx, job, err)
		return
	}

	r.markDone(ctx, job)
}

func (r *OutboxDelivery) markDone(ctx context.Context, job email.ClaimedItem) {
	if err := r.outbox.MarkSent(ctx, job.ID, r.now()); err != nil {
		r.logger.ErrorContext(ctx, "mark outbox sent failed", "error", err)
	}
}

func (r *OutboxDelivery) fail(ctx context.Context, job email.ClaimedItem, err error) {
	now := r.now()
	lastErr := truncateLastError(err)

	if job.Attempts >= int32(r.maxAttempts) {
		if err := r.outbox.MarkFailed(ctx, job.ID, now, lastErr); err != nil {
			r.logger.ErrorContext(ctx, "failed to mark outbox failed", "error", err)
		}
		return
	}

	if err := r.outbox.Reschedule(ctx, job.ID, now.Add(r.backoff(job.Attempts)), lastErr); err != nil {
		r.logger.ErrorContext(ctx, "reschedule outbox failed", "error", err)
	}
}

func (r *OutboxDelivery) backoff(attempts int32) time.Duration {
	d := r.retryBase << (attempts - 1)
	if d > r.retryCap || d <= 0 {
		return r.retryCap
	}
	return d
}

const maxLastError = 500

func truncateLastError(err error) string {
	s := strings.ToValidUTF8(err.Error(), "")
	r := []rune(s)

	if len(r) > maxLastError {
		return string(r[:maxLastError-1]) + "…"
	}
	return s
}
