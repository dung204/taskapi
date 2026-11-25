package email

import "context"

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

// Mailer delivers emails.
type Mailer interface {
	// Send delivers a [Message] to its recipient. The message needs a valid
	// recipient and a text body, an HTML body, or both.
	//
	// Returns the following errors:
	//   - an error - if the message is invalid or could not be delivered.
	Send(ctx context.Context, msg Message) error
}
