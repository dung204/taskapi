package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hako/durafmt"

	"github.com/dung204/taskapi/internal/auth/templates"
	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/opaquetoken"
)

type EmailBuilder struct {
	baseURL          string
	verifyEmailTTL   time.Duration
	resetPasswordTTL time.Duration
	users            UserStore
	userTokens       UserTokenStore
	templates        email.Templates
	now              func() time.Time
}

type EmailBuilderDeps struct {
	BaseURL          string
	VerifyEmailTTL   time.Duration
	ResetPasswordTTL time.Duration
	Users            UserStore
	UserTokens       UserTokenStore
	Now              func() time.Time
}

func NewEmailBuilder(deps EmailBuilderDeps) (*EmailBuilder, error) {
	tmpls, err := email.LoadTemplates(templates.FS)
	if err != nil {
		return nil, fmt.Errorf("new email builder: %w", err)
	}

	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &EmailBuilder{
		baseURL:          deps.BaseURL,
		verifyEmailTTL:   deps.VerifyEmailTTL,
		resetPasswordTTL: deps.ResetPasswordTTL,
		users:            deps.Users,
		userTokens:       deps.UserTokens,
		templates:        tmpls,
		now:              deps.Now,
	}, nil
}

type emailPayload struct {
	Email     string
	Link      string
	ExpiresIn string
}

func (b *EmailBuilder) Build(ctx context.Context, item email.ClaimedItem) (email.Message, error) {
	user, err := b.users.GetByID(ctx, item.UserID)
	if err != nil {
		return email.Message{}, email.ErrNothingToSend
	}

	// Most kinds carry a one-time link; the "already registered" notice only
	// points at the sign-in page and creates no token.
	var (
		token    []byte
		payload  = emailPayload{Email: user.Email}
		tokenTTL = b.getTokenTTL(item.Kind)
	)
	switch item.Kind {
	case email.KindAlreadyRegistered:
		payload.Link = b.baseURL + "/login"
	case email.KindVerifyEmail, email.KindResetPassword:
		token, err = opaquetoken.Generate()
		if err != nil {
			return email.Message{}, err
		}
		payload.Link = fmt.Sprintf("%s/%s?token=%s", b.baseURL, getKindURLSlug(item.Kind), opaquetoken.Encode(token))
		payload.ExpiresIn = durafmt.Parse(tokenTTL).String()
	default:
		return email.Message{}, fmt.Errorf("build email: unsupported kind %s", item.Kind)
	}

	message, err := b.templates.Render(item.Kind, payload)
	if err != nil {
		return email.Message{}, fmt.Errorf("build email: %w", err)
	}

	message.To = user.Email
	message.Subject = getKindSubject(item.Kind)

	if token == nil {
		return message, nil
	}

	now := b.now()
	tokenHash := opaquetoken.Hash(token)

	userToken, err := NewUserToken(NewUserTokenParams{
		UserID:    user.ID,
		Purpose:   TokenPurpose(item.Kind),
		TokenHash: tokenHash,
		TTL:       tokenTTL,
	}, now)
	if err != nil {
		return email.Message{}, fmt.Errorf("build email: %w", err)
	}

	if err := b.userTokens.Create(ctx, userToken); err != nil {
		return email.Message{}, fmt.Errorf("build email: %w", err)
	}

	return message, nil
}

var (
	kindVerifyEmailURLSlug   = strings.ReplaceAll(string(email.KindVerifyEmail), "_", "-")
	kindResetPasswordURLSlug = strings.ReplaceAll(string(email.KindResetPassword), "_", "-")
)

func getKindURLSlug(k email.Kind) string {
	switch k {
	case email.KindVerifyEmail:
		return kindVerifyEmailURLSlug
	case email.KindResetPassword:
		return kindResetPasswordURLSlug
	default:
		return "unknown"
	}
}

func getKindSubject(k email.Kind) string {
	switch k {
	case email.KindVerifyEmail:
		return "Verify your email address"
	case email.KindResetPassword:
		return "Reset your password"
	case email.KindAlreadyRegistered:
		return "You already have an account"
	default:
		return ""
	}
}

func (b *EmailBuilder) getTokenTTL(k email.Kind) time.Duration {
	switch k {
	case email.KindVerifyEmail:
		return b.verifyEmailTTL
	case email.KindResetPassword:
		return b.resetPasswordTTL
	default:
		return 0
	}
}
