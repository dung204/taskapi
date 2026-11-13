package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	htmltmpl "html/template"
	"io/fs"
	"strings"
	texttmpl "text/template"
	"time"

	"github.com/hako/durafmt"

	"github.com/dung204/taskapi/internal/auth/templates"
	"github.com/dung204/taskapi/internal/email"
)

type EmailBuilder struct {
	baseURL          string
	verifyEmailTTL   time.Duration
	resetPasswordTTL time.Duration
	users            UserStore
	userTokens       UserTokenStore
	htmlTemplates    map[string]*htmltmpl.Template
	textTemplates    map[string]*texttmpl.Template
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
	entries, err := fs.ReadDir(templates.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("new email builder: can not read embed FS: %w", err)
	}

	htmlTmpls := make(map[string]*htmltmpl.Template)
	textTmpls := make(map[string]*texttmpl.Template)

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		switch {
		case strings.HasSuffix(name, ".html.tmpl"):
			key := strings.TrimSuffix(name, ".html.tmpl")

			t, err := htmltmpl.ParseFS(templates.FS, name)
			if err != nil {
				return nil, fmt.Errorf("parse template: %w", err)
			}

			htmlTmpls[key] = t

		case strings.HasSuffix(name, ".text.tmpl") || strings.HasSuffix(name, ".tmpl"):
			key := strings.TrimSuffix(name, ".text.tmpl")
			key = strings.TrimSuffix(key, ".tmpl")

			t, err := texttmpl.ParseFS(templates.FS, name)
			if err != nil {
				return nil, fmt.Errorf("parse template: %w", err)
			}

			textTmpls[key] = t
		}
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
		htmlTemplates:    htmlTmpls,
		textTemplates:    textTmpls,
		now:              deps.Now,
	}, nil
}

type emailPayload struct {
	Email     string
	Link      string
	ExpiresIn string
}

func (b *EmailBuilder) Build(ctx context.Context, item email.ClaimedItem) (email.Message, error) {
	htmlTmpl, hasHTMLTemplate := b.htmlTemplates[string(item.Kind)]
	textTmpl, hasTextTemplate := b.textTemplates[string(item.Kind)]

	if !hasHTMLTemplate && !hasTextTemplate {
		return email.Message{}, errors.New("build email: message has no body")
	}

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
	if item.Kind == email.KindAlreadyRegistered {
		payload.Link = b.baseURL + "/login"
	} else {
		token, err = generateOpaqueToken()
		if err != nil {
			return email.Message{}, err
		}
		payload.Link = fmt.Sprintf("%s/%s?token=%s", b.baseURL, getKindURLSlug(item.Kind), base64.RawURLEncoding.EncodeToString(token))
		payload.ExpiresIn = durafmt.Parse(tokenTTL).String()
	}

	message := email.Message{
		To:      user.Email,
		Subject: getKindSubject(item.Kind),
	}

	if hasHTMLTemplate {
		var buf bytes.Buffer
		if err := htmlTmpl.Execute(&buf, payload); err != nil {
			return email.Message{}, fmt.Errorf("build email: %w", err)
		}

		message.HTML = buf.String()
	}

	if hasTextTemplate {
		var buf bytes.Buffer
		if err := textTmpl.Execute(&buf, payload); err != nil {
			return email.Message{}, fmt.Errorf("build email: %w", err)
		}

		message.Text = buf.String()
	}

	if token == nil {
		return message, nil
	}

	now := b.now()
	sha256Token := sha256.Sum256(token)

	userToken, err := NewUserToken(NewUserTokenParams{
		UserID:    user.ID,
		Purpose:   TokenPurpose(item.Kind),
		TokenHash: sha256Token[:],
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
