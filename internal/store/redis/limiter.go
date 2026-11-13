package redis

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
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

var attemptScript = goredis.NewScript(`
local lock_ttl = redis.call('PTTL', KEYS[2])
if lock_ttl > 0 then
	return lock_ttl
end

local n = redis.call('INCR', KEYS[1])
redis.call('PEXPIRE', KEYS[1], ARGV[1])

local free = tonumber(ARGV[2])
local steps = #ARGV - 2
local idx = n - free
if idx >= 1 then
	if idx > steps then idx = steps end
	redis.call('SET', KEYS[2], '1', 'PX', tonumber(ARGV[2 + idx]))
end

return 0
`)

func (l *LoginLimiter) Attempt(ctx context.Context, email string) (wait time.Duration, err error) {
	h := hashEmail(email)
	keys := []string{"login:cnt:" + h, "login:lock:" + h}
	args := []any{l.window.Milliseconds(), l.freeAttempts}
	for _, v := range l.backoff {
		args = append(args, v.Milliseconds())
	}

	ms, err := attemptScript.Run(ctx, l.client, keys, args...).Int64()
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

type MailRequestLimiter struct {
	client           *goredis.Client
	requestCooldown  time.Duration
	requestHourlyMax int
}

type MailRequestLimiterOpts struct {
	RequestCooldown  time.Duration
	RequestHourlyMax int
}

func NewMailRequestLimiter(client *goredis.Client, opts MailRequestLimiterOpts) *MailRequestLimiter {
	return &MailRequestLimiter{
		client:           client,
		requestCooldown:  opts.RequestCooldown,
		requestHourlyMax: opts.RequestHourlyMax,
	}
}

var allowScript = goredis.NewScript(`
local cooldownLeft = redis.call('PTTL', KEYS[1])
if cooldownLeft > 0 then
	return cooldownLeft
end

local n = redis.call('INCR', KEYS[2])
if n == 1 then
	redis.call('PEXPIRE', KEYS[2], tonumber(ARGV[3]))
end

if n > tonumber(ARGV[1]) then
	local left = redis.call('PTTL', KEYS[2])
	if left < 1 then
		left = 1
	end
	return left
end

redis.call('SET', KEYS[1], '1', 'PX', tonumber(ARGV[2]))
return 0
`)

const mailRequestWindow = time.Hour

// Allow implements [auth.MailRequestLimiter].
func (l *MailRequestLimiter) Allow(ctx context.Context, email string) (retryAfter time.Duration, err error) {
	h := hashEmail(email)
	keys := []string{"mail:cool:" + h, "mail:hour:" + h}
	args := []any{l.requestHourlyMax, l.requestCooldown.Milliseconds(), mailRequestWindow.Milliseconds()}

	ms, err := allowScript.Eval(ctx, l.client, keys, args...).Int64()
	if err != nil {
		return 0, fmt.Errorf("mail request limiter allow: %w", err)
	}

	return time.Duration(ms) * time.Millisecond, nil
}

func hashEmail(email string) string {
	hash := sha256.Sum256([]byte(email))
	return string(hash[:])
}
