package email

import (
	"context"
	"fmt"
)

// MessageBuilder turns a claimed outbox item into the email to send.
type MessageBuilder interface {
	// Build writes the [Message] for item.
	//
	// Returns:
	//   - msg - the email to send.
	//
	// Returns the following errors:
	//   - [ErrNothingToSend] - if there is nothing to send any more (for
	//     example the user no longer exists); the worker marks the item as sent.
	//   - an error - for any other failure; the worker retries the item later or
	//     gives up on it.
	Build(ctx context.Context, item ClaimedItem) (msg Message, err error)
}

// Router is a [MessageBuilder] that hands each item to the builder registered
// for its [Kind], so packages that own different kinds (auth, task) can share
// one outbox worker.
type Router map[Kind]MessageBuilder

var _ MessageBuilder = Router(nil)

// Build implements [MessageBuilder]. It returns an error if no builder is
// registered for the kind of item.
func (r Router) Build(ctx context.Context, item ClaimedItem) (Message, error) {
	b, ok := r[item.Kind]
	if !ok {
		return Message{}, fmt.Errorf("build email: no builder for kind %s", item.Kind)
	}

	return b.Build(ctx, item)
}
