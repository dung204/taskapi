package email

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/wneessen/go-mail"
)

type TLSMode string

const (
	TLSStartTLS      TLSMode = "starttls"
	TLSOpportunistic TLSMode = "opportunistic"
	TLSNone          TLSMode = "none"
)

func (m TLSMode) mailPolicy() (mail.TLSPolicy, error) {
	switch m {
	case TLSStartTLS:
		return mail.TLSMandatory, nil
	case TLSOpportunistic:
		return mail.TLSOpportunistic, nil
	case TLSNone:
		return mail.NoTLS, nil
	default:
		return 0, fmt.Errorf("unknown tls mode %q", m)
	}
}

// authType picks the SMTP AUTH mechanism. Over an encrypted connection the
// client discovers the best one the server offers. Without encryption, go-mail's
// discovery only accepts mechanisms that never send the password in clear
// (SCRAM, CRAM-MD5), which dev servers such as Mailpit do not offer: they only
// have PLAIN and LOGIN. TLSNone is an explicit "development only" choice, so it
// opts in to PLAIN over the unencrypted connection.
func (m TLSMode) authType() mail.SMTPAuthType {
	if m == TLSNone {
		return mail.SMTPAuthPlainNoEnc
	}
	return mail.SMTPAuthAutoDiscover
}

type SMTPConfig struct {
	Host     string
	Port     int
	TLS      TLSMode
	Timeout  time.Duration
	Username string
	Password string
	From     string
}

type SMTPMailer struct {
	client *mail.Client
	from   string
}

var _ Mailer = (*SMTPMailer)(nil)

func NewSMTPMailer(cfg SMTPConfig) (*SMTPMailer, error) {
	policy, err := cfg.TLS.mailPolicy()
	if err != nil {
		return nil, fmt.Errorf("new smtp mailer: %w", err)
	}

	client, err := mail.NewClient(
		cfg.Host,
		mail.WithPort(cfg.Port),
		mail.WithTLSPolicy(policy),
		mail.WithTimeout(cfg.Timeout),
		mail.WithSMTPAuth(cfg.TLS.authType()),
		mail.WithUsername(cfg.Username),
		mail.WithPassword(cfg.Password),
	)
	if err != nil {
		return nil, fmt.Errorf("new smtp mailer: fail to create client: %w", err)
	}

	if err := mail.NewMsg().From(cfg.From); err != nil {
		return nil, fmt.Errorf("new smtp mailer: invalid from %q: %w", cfg.From, err)
	}

	return &SMTPMailer{
		client: client,
		from:   cfg.From,
	}, nil
}

// Send implements [Mailer].
func (s *SMTPMailer) Send(ctx context.Context, msg Message) error {
	message := mail.NewMsg()

	if err := message.From(s.from); err != nil {
		return fmt.Errorf("send mail: invalid from %q: %w", s.from, err)
	}

	if err := message.To(msg.To); err != nil {
		return fmt.Errorf("send mail: invalid to %q: %w", msg.To, err)
	}

	message.Subject(msg.Subject)

	switch {
	case msg.Text == "" && msg.HTML == "":
		return errors.New("send mail: text or HTML is required")
	case msg.Text != "" && msg.HTML != "":
		message.SetBodyString(mail.TypeTextPlain, msg.Text)
		message.AddAlternativeString(mail.TypeTextHTML, msg.HTML)
	case msg.Text != "":
		message.SetBodyString(mail.TypeTextPlain, msg.Text)
	case msg.HTML != "":
		message.SetBodyString(mail.TypeTextHTML, msg.HTML)
	}

	if err := s.client.DialAndSendWithContext(ctx, message); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}

	return nil
}
