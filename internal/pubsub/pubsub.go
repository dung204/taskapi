// Package pubsub defines a minimal publish/subscribe abstraction: events
// are addressed by topic, and consumers receive them on a channel.
//
// The package only declares the consumer side (Subscriber). Concrete
// implementations (for example an in-process broker or a Redis-backed one)
// live elsewhere and satisfy the interface.
package pubsub

import "context"

// Event is a single message delivered to a subscriber.
type Event struct {
	// Topic is the topic the event was published to.
	Topic string
	// Payload is the opaque message body. Its encoding is a contract between
	// the publisher and the subscriber; this package does not interpret it.
	Payload []byte
}

// Subscriber receives events published to a topic.
type Subscriber interface {
	// Subscribe starts listening on topic and returns a channel of events.
	//
	// The subscription lasts until ctx is canceled. Implementations close the
	// returned channel once the subscription ends, so callers can range over
	// it. If the subscription cannot be established, Subscribe returns a
	// non-nil error and a nil channel.
	Subscribe(ctx context.Context, topic string) (<-chan Event, error)
}
