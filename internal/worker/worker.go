// Package worker holds the background jobs of the server. Each job implements
// [Runner]: it is started in its own goroutine and runs until its context is
// cancelled, which is how graceful shutdown stops it.
//
// The jobs are:
//
//   - [OverdueScan] ticks at a fixed interval and marks overdue tasks through
//     [task.Store.MarkOverdue]. A panic in one scan is recovered and logged, and
//     the loop keeps going.
//   - [OutboxDelivery] sends the queued emails of the outbox. It wakes up when it
//     is notified of new items and, as a fallback, on a timer, so an item is not
//     left waiting if a notification is lost.
//
// The package only depends on small interfaces for what it uses ([OutboxQueue],
// [email.MessageBuilder], [Mailer]), so a job never sees a concrete backend. Anything
// with a matching Run method can be run alongside them, for example the in-memory
// rate limiter's cleanup loop.
package worker

import "context"

// Runner is a background job.
type Runner interface {
	// Run does the job until ctx is cancelled. It blocks, so callers start it
	// with go.
	Run(ctx context.Context)
}
