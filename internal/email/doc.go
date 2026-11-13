// Package email is the outgoing-email domain: the [Message] to deliver, the
// [Mailer] that delivers it, and the outbox that records which emails are still
// to be sent.
//
// Delivery is split in two so a request never waits on, or fails because of, a
// mail server. A caller that needs an email sent (email verification, password
// reset) does not send it: it creates an [OutboxItem] with [NewOutboxItem],
// which says who gets which [Kind] of email, and stores it through
// [OutboxStore]. A background worker later claims due items for a limited
// lease, turns each into a [Message], sends it with a [Mailer], and then marks
// the item sent or reschedules it with the error. An item that is claimed but
// never finished becomes claimable again when its lease runs out, so delivery
// is at least once: a mail can be sent twice, never silently lost.
//
// [SMTPMailer] is the [Mailer] used in production, configured with
// [SMTPConfig] and a [TLSMode]. Tests can use any other implementation of the
// one-method interface.
//
// The package knows nothing about HTTP, SQL or how an email's subject and body
// are written. Rendering is the worker's job, and storage backends implement
// [OutboxStore]. Invalid input is reported by wrapping
// [github.com/dung204/taskapi/internal/apperr.ErrInvalidInput], as with
// [ErrInvalidKind].
package email
