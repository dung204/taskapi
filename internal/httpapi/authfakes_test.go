package httpapi

// Fakes for the auth service's dependencies that are not Postgres (clock,
// revoker, limiters, hasher, mailer), used by setupAuthTest. The users,
// sessions, tokens and outbox are the real Postgres stores.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/email"
)

// testClock is the server clock of the tests; only Advance moves it.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock {
	return &testClock{t: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// mailbox plays the part of the outbox worker and the mailer. The auth service
// queues its emails in the real outbox table; deliver turns the rows queued for
// an address into sentMails, with a fresh one-time token (stored through the
// real token store) when the kind needs one.
type mailbox struct {
	pool   *pgxpool.Pool
	tokens auth.UserTokenStore

	mu    sync.Mutex
	seen  map[uuid.UUID]bool
	mails map[string][]sentMail
}

func newMailbox(pool *pgxpool.Pool, tokens auth.UserTokenStore) *mailbox {
	return &mailbox{pool: pool, tokens: tokens, seen: make(map[uuid.UUID]bool), mails: make(map[string][]sentMail)}
}

// to returns, oldest first, the emails sent to the address so far.
func (m *mailbox) to(tb testing.TB, address string, now time.Time) []sentMail {
	tb.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()

	address = strings.ToLower(address)
	rows, err := m.pool.Query(tb.Context(),
		`SELECT id, user_id, kind FROM email_outbox WHERE lower(to_email) = $1 ORDER BY created_at, id`, address)
	if err != nil {
		tb.Fatalf("query the outbox: %v", err)
	}
	type queued struct {
		id, userID uuid.UUID
		kind       email.Kind
	}
	var items []queued
	for rows.Next() {
		var q queued
		if err := rows.Scan(&q.id, &q.userID, &q.kind); err != nil {
			rows.Close()
			tb.Fatalf("scan the outbox: %v", err)
		}
		items = append(items, q)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		tb.Fatalf("read the outbox: %v", err)
	}

	for _, q := range items {
		if m.seen[q.id] {
			continue
		}
		m.seen[q.id] = true
		mail := sentMail{Kind: string(q.kind)}

		if purpose, ok := tokenPurposeOf(q.kind); ok {
			raw := make([]byte, sha256.Size)
			if _, err := rand.Read(raw); err != nil {
				tb.Fatalf("generate token: %v", err)
			}
			hash := sha256.Sum256(raw)
			tok, err := auth.NewUserToken(auth.NewUserTokenParams{
				UserID: q.userID, Purpose: purpose, TokenHash: hash[:], TTL: fakeTokenTTL(purpose),
			}, now)
			if err != nil {
				tb.Fatalf("new user token: %v", err)
			}
			if err := m.tokens.Create(tb.Context(), tok); err != nil {
				tb.Fatalf("create user token: %v", err)
			}
			mail.Token = base64.RawURLEncoding.EncodeToString(raw)
		}

		m.mails[address] = append(m.mails[address], mail)
	}

	return slices.Clone(m.mails[address])
}

// fakeTokenTTL mirrors the defaults of VERIFY_EMAIL_TOKEN_TTL and
// RESET_PASSWORD_TOKEN_TTL; the expiry tests advance the clock past them.
func fakeTokenTTL(p auth.TokenPurpose) time.Duration {
	if p == auth.PurposeResetPassword {
		return time.Hour
	}
	return 24 * time.Hour
}

func tokenPurposeOf(k email.Kind) (auth.TokenPurpose, bool) {
	switch k {
	case email.KindVerifyEmail:
		return auth.PurposeVerifyEmail, true
	case email.KindResetPassword:
		return auth.PurposeResetPassword, true
	default:
		return "", false
	}
}

// --- revocation --------------------------------------------------------------

// fakeRevoker implements [auth.Revoker] and the middleware's revocation check
// with the same rules as the Redis store.
type fakeRevoker struct {
	mu         sync.Mutex
	sessions   map[uuid.UUID]bool
	validAfter map[uuid.UUID]time.Time
}

var _ auth.Revoker = (*fakeRevoker)(nil)

func newFakeRevoker() *fakeRevoker {
	return &fakeRevoker{sessions: make(map[uuid.UUID]bool), validAfter: make(map[uuid.UUID]time.Time)}
}

func (r *fakeRevoker) RevokeSession(_ context.Context, sid uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[sid] = true
	return nil
}

func (r *fakeRevoker) RevokeUser(_ context.Context, userID uuid.UUID, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validAfter[userID] = at
	return nil
}

func (r *fakeRevoker) Check(_ context.Context, sid, userID uuid.UUID, iat time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions[sid] {
		return true, nil
	}
	if at, ok := r.validAfter[userID]; ok && iat.Unix() < at.Unix() {
		return true, nil
	}
	return false, nil
}

// --- limiters ----------------------------------------------------------------

// fakeLoginLimiter lets freeAttempts logins per email through, then refuses the
// email for lockFor. A successful login resets it.
type fakeLoginLimiter struct {
	mu           sync.Mutex
	now          func() time.Time
	freeAttempts int
	lockFor      time.Duration
	counts       map[string]int
	lockedUntil  map[string]time.Time
}

var _ auth.LoginLimiter = (*fakeLoginLimiter)(nil)

func newFakeLoginLimiter(now func() time.Time, freeAttempts int, lockFor time.Duration) *fakeLoginLimiter {
	return &fakeLoginLimiter{
		now: now, freeAttempts: freeAttempts, lockFor: lockFor,
		counts: make(map[string]int), lockedUntil: make(map[string]time.Time),
	}
}

func (l *fakeLoginLimiter) Attempt(_ context.Context, mail string) (time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if until := l.lockedUntil[mail]; until.After(now) {
		return until.Sub(now), nil
	}
	l.counts[mail]++
	if l.counts[mail] > l.freeAttempts {
		l.lockedUntil[mail] = now.Add(l.lockFor)
	}
	return 0, nil
}

func (l *fakeLoginLimiter) Reset(_ context.Context, mail string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.counts, mail)
	delete(l.lockedUntil, mail)
	return nil
}

// fakeMailLimiter allows maxPerHour mail requests per address per hour of the
// test clock.
type fakeMailLimiter struct {
	mu         sync.Mutex
	now        func() time.Time
	maxPerHour int
	requests   map[string][]time.Time
}

var _ auth.AuthMailLimiter = (*fakeMailLimiter)(nil)

func newFakeMailLimiter(now func() time.Time, maxPerHour int) *fakeMailLimiter {
	return &fakeMailLimiter{now: now, maxPerHour: maxPerHour, requests: make(map[string][]time.Time)}
}

func (l *fakeMailLimiter) Allow(_ context.Context, mail string) (time.Duration, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	recent := slices.DeleteFunc(l.requests[mail], func(t time.Time) bool { return !t.After(now.Add(-time.Hour)) })
	if len(recent) >= l.maxPerHour {
		l.requests[mail] = recent
		return recent[0].Add(time.Hour).Sub(now), nil
	}
	l.requests[mail] = append(recent, now)
	return 0, nil
}

// --- fast hasher and keys ----------------------------------------------------

// fakeHasher is a stand-in for argon2 that is instant and still distinguishes
// passwords.
type fakeHasher struct{}

var _ auth.Hasher = fakeHasher{}

func (fakeHasher) Hash(_ context.Context, password string) (string, error) {
	return "hashed:" + password, nil
}

func (fakeHasher) Verify(_ context.Context, password, encodedHash string) (bool, error) {
	return encodedHash == "hashed:"+password, nil
}

func newTestKeys(tb testing.TB) (ed25519.PublicKey, ed25519.PrivateKey) {
	tb.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		tb.Fatalf("generate key: %v", err)
	}
	return pub, priv
}
