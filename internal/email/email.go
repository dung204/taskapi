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

import (
	"fmt"
	"regexp"
	"strings"
)

// maxAddressLen is the longest email address accepted, in bytes (the SMTP
// path limit of RFC 5321).
const maxAddressLen = 254

var emailRegex = regexp.MustCompile(`^[a-z0-9._%+\-]+@[a-z0-9-]+(\.[a-z0-9-]+)*\.[a-z]{2,}$`)

// Parse trims and lowercases email and checks that it looks like a valid
// address of at most 254 bytes. It returns the normalized address, or an error
// wrapping [ErrInvalidEmail].
func Parse(email string) (string, error) {
	mail := strings.ToLower(strings.TrimSpace(email))
	if !emailRegex.MatchString(mail) || len(mail) > maxAddressLen {
		return "", fmt.Errorf("%w: email must be a valid email and have at most %d characters", ErrInvalidEmail, maxAddressLen)
	}
	return mail, nil
}
