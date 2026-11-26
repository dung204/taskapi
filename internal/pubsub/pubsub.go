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
	// Subscribe starts listening on topic. The subscription lasts until ctx is
	// canceled; implementations close the returned channel once it ends, so
	// callers can range over it.
	//
	// Returns:
	//   - events - the channel on which the [Event]s of the topic are delivered.
	//     It is nil if the subscription could not be established.
	//
	// Returns the following errors:
	//   - a non-nil error - if the subscription could not be established.
	Subscribe(ctx context.Context, topic string) (events <-chan Event, err error)
}
