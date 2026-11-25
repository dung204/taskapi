package redis

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"time"
	"uuid"

	goredis "github.com/redis/go-redis/v9"
)

var (
	//go:embed lua/login_attempt.lua
	loginAttemptSrc string
	//go:embed lua/mail_allow.lua
	mailAllowSrc string
	//go:embed lua/owner_invite.lua
	ownerInviteSrc string
)

var (
	loginAttemptScript = goredis.NewScript(loginAttemptSrc)
	mailAllowScript    = goredis.NewScript(mailAllowSrc)
	ownerInviteScript  = goredis.NewScript(ownerInviteSrc)
)

type LoginLimiter struct {
	client       *goredis.Client
	freeAttempts int             // positive
	backoff      []time.Duration // final elem is "the ceil"
	window       time.Duration   // window >= "the ceil" (final elem of backoff)
}

type LoginLimiterOpts struct {
	FreeAttempts int
	Backoff      []time.Duration
	Window       time.Duration
}

func NewLoginLimiter(client *goredis.Client, opts LoginLimiterOpts) *LoginLimiter {
	return &LoginLimiter{
		client:       client,
		freeAttempts: opts.FreeAttempts,
		backoff:      opts.Backoff,
		window:       opts.Window,
	}
}

func (l *LoginLimiter) Attempt(ctx context.Context, email string) (wait time.Duration, err error) {
	h := hashEmail(email)
	keys := []string{"login:cnt:" + h, "login:lock:" + h}
	args := []any{l.window.Milliseconds(), l.freeAttempts}
	for _, v := range l.backoff {
		args = append(args, v.Milliseconds())
	}

	ms, err := loginAttemptScript.Run(ctx, l.client, keys, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("login limiter attempt: %w", err)
	}

	return time.Duration(ms) * time.Millisecond, nil
}

func (l *LoginLimiter) Reset(ctx context.Context, email string) error {
	h := hashEmail(email)
	keys := []string{"login:cnt:" + h, "login:lock:" + h}

	if err := l.client.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("login limiter reset: %w", err)
	}

	return nil
}

type AuthMailLimiter struct {
	client           *goredis.Client
	requestCooldown  time.Duration
	requestHourlyMax int
}

type AuthMailLimiterOpts struct {
	RequestCooldown  time.Duration
	RequestHourlyMax int
}

func NewAuthMailLimiter(client *goredis.Client, opts AuthMailLimiterOpts) *AuthMailLimiter {
	return &AuthMailLimiter{
		client:           client,
		requestCooldown:  opts.RequestCooldown,
		requestHourlyMax: opts.RequestHourlyMax,
	}
}

const authMailWindow = time.Hour

// Allow implements [auth.MailRequestLimiter].
func (l *AuthMailLimiter) Allow(ctx context.Context, email string) (retryAfter time.Duration, err error) {
	h := hashEmail(email)
	keys := []string{"authmail:cool:" + h, "authmail:hour:" + h}
	args := []any{l.requestHourlyMax, l.requestCooldown.Milliseconds(), authMailWindow.Milliseconds()}

	ms, err := mailAllowScript.Eval(ctx, l.client, keys, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("auth mail limiter allow: %w", err)
	}

	return time.Duration(ms) * time.Millisecond, nil
}

type InviteMailLimiter struct {
	client           *goredis.Client
	requestCooldown  time.Duration
	requestHourlyMax int
}

type InviteMailLimiterOpts struct {
	RequestCooldown  time.Duration
	RequestHourlyMax int
}

func NewInviteMailLimiter(client *goredis.Client, opts InviteMailLimiterOpts) *InviteMailLimiter {
	return &InviteMailLimiter{
		client:           client,
		requestCooldown:  opts.RequestCooldown,
		requestHourlyMax: opts.RequestHourlyMax,
	}
}

const inviteMailWindow = time.Hour

// Allow implements [auth.MailRequestLimiter].
func (l *InviteMailLimiter) Allow(ctx context.Context, email string) (retryAfter time.Duration, err error) {
	h := hashEmail(email)
	keys := []string{"invite:cool:" + h, "invite:hour:" + h}
	args := []any{l.requestHourlyMax, l.requestCooldown.Milliseconds(), inviteMailWindow.Milliseconds()}

	ms, err := mailAllowScript.Eval(ctx, l.client, keys, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("invite mail limiter allow: %w", err)
	}

	return time.Duration(ms) * time.Millisecond, nil
}

func hashEmail(email string) string {
	hash := sha256.Sum256([]byte(email))
	return string(hash[:])
}

type OwnerInviteLimiter struct {
	client          *goredis.Client
	inviteHourlyMax int
}

type OwnerInviteLimiterOpts struct {
	InviteHourlyMax int
}

func NewOwnerInviteLimiter(client *goredis.Client, opts OwnerInviteLimiterOpts) *OwnerInviteLimiter {
	return &OwnerInviteLimiter{
		client:          client,
		inviteHourlyMax: opts.InviteHourlyMax,
	}
}

const ownerInviteWindow = time.Hour

// Allow implements [task.OwnerInviteLimiter].
func (l *OwnerInviteLimiter) Allow(ctx context.Context, owner uuid.UUID) (retryAfter time.Duration, err error) {
	keys := []string{"invite:owner:" + owner.String()}
	args := []any{l.inviteHourlyMax, ownerInviteWindow.Milliseconds()}

	ms, err := ownerInviteScript.Run(ctx, l.client, keys, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("owner invite limiter allow: %w", err)
	}

	return time.Duration(ms) * time.Millisecond, nil
}
