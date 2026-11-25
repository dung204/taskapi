package httpapi

// In-memory fakes for the auth service's dependencies, used by setupAuthTest.
// They follow the contracts that authtest checks on the real stores, but are
// only as complete as the HTTP tests need.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

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

// fakeDB holds the rows of all the fake stores behind one lock.
type fakeDB struct {
	mu       sync.Mutex
	users    map[uuid.UUID]auth.User
	sessions map[uuid.UUID]auth.Session
	tokens   []auth.UserToken
	outbox   []email.OutboxItem
	handled  int // outbox items already turned into mails
	mails    map[string][]sentMail
}

func newFakeDB() *fakeDB {
	return &fakeDB{
		users:    make(map[uuid.UUID]auth.User),
		sessions: make(map[uuid.UUID]auth.Session),
		mails:    make(map[string][]sentMail),
	}
}

func (db *fakeDB) stores() auth.Stores {
	return auth.Stores{
		Users:      fakeUsers{db},
		UserTokens: fakeUserTokens{db},
		Sessions:   fakeSessions{db},
		Outbox:     fakeOutbox{db},
	}
}

// WithTx implements [auth.transactor]. There is no rollback: the tests do not
// inject failures into a transaction.
func (db *fakeDB) WithTx(_ context.Context, fn func(auth.Stores) error) error {
	return fn(db.stores())
}

// deliver plays the part of the outbox worker and the mailer: every pending
// outbox item becomes a sentMail, with a fresh one-time token when the kind
// needs one.
func (db *fakeDB) deliver(tb testing.TB, now time.Time) {
	tb.Helper()
	db.mu.Lock()
	defer db.mu.Unlock()

	for ; db.handled < len(db.outbox); db.handled++ {
		item := db.outbox[db.handled]
		m := sentMail{Kind: string(item.Kind)}

		if purpose, ok := tokenPurposeOf(item.Kind); ok {
			raw := make([]byte, sha256.Size)
			if _, err := rand.Read(raw); err != nil {
				tb.Fatalf("generate token: %v", err)
			}
			hash := sha256.Sum256(raw)
			tok, err := auth.NewUserToken(auth.NewUserTokenParams{
				UserID: item.UserID, Purpose: purpose, TokenHash: hash[:], TTL: fakeTokenTTL(purpose),
			}, now)
			if err != nil {
				tb.Fatalf("new user token: %v", err)
			}
			db.tokens = append(db.tokens, tok)
			m.Token = base64.RawURLEncoding.EncodeToString(raw)
		}

		db.mails[item.ToEmail] = append(db.mails[item.ToEmail], m)
	}
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

// --- users -------------------------------------------------------------------

type fakeUsers struct{ db *fakeDB }

var _ auth.UserStore = fakeUsers{}

func (s fakeUsers) Create(_ context.Context, u auth.User) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	for _, other := range s.db.users {
		if other.Email == u.Email {
			return auth.ErrEmailTaken
		}
	}
	s.db.users[u.ID] = u
	return nil
}

func (s fakeUsers) GetByID(_ context.Context, id uuid.UUID) (auth.User, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	u, ok := s.db.users[id]
	if !ok {
		return auth.User{}, auth.ErrUserNotFound
	}
	return u, nil
}

func (s fakeUsers) GetByEmail(_ context.Context, mail string) (auth.User, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	for _, u := range s.db.users {
		if u.Email == mail {
			return u, nil
		}
	}
	return auth.User{}, auth.ErrUserNotFound
}

func (s fakeUsers) update(id uuid.UUID, fn func(*auth.User)) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	u, ok := s.db.users[id]
	if !ok {
		return auth.ErrUserNotFound
	}
	fn(&u)
	s.db.users[id] = u
	return nil
}

func (s fakeUsers) SetEmailVerified(_ context.Context, id uuid.UUID, at time.Time) error {
	return s.update(id, func(u *auth.User) { u.EmailVerifiedAt = &at; u.UpdatedAt = at })
}

func (s fakeUsers) UpdatePassword(_ context.Context, id uuid.UUID, hash string, at time.Time) error {
	return s.update(id, func(u *auth.User) { u.PasswordHash = &hash; u.UpdatedAt = at })
}

func (s fakeUsers) SetTokensValidAfter(_ context.Context, id uuid.UUID, after time.Time) error {
	return s.update(id, func(u *auth.User) { u.TokensValidAfter = &after; u.UpdatedAt = after })
}

// --- sessions ----------------------------------------------------------------

type fakeSessions struct{ db *fakeDB }

var _ auth.SessionStore = fakeSessions{}

func (s fakeSessions) Create(_ context.Context, ss auth.Session) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	s.db.sessions[ss.ID] = ss
	return nil
}

// GetByRefreshTokenHash finds a session by its current or its previous hash.
func (s fakeSessions) GetByRefreshTokenHash(_ context.Context, hash []byte) (auth.Session, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	for _, ss := range s.db.sessions {
		if bytes.Equal(ss.RefreshTokenHash, hash) ||
			(len(ss.PreviousRefreshTokenHash) > 0 && bytes.Equal(ss.PreviousRefreshTokenHash, hash)) {
			return ss, nil
		}
	}
	return auth.Session{}, auth.ErrSessionNotFound
}

func (s fakeSessions) Rotate(_ context.Context, id uuid.UUID, oldHash, newHash []byte, now time.Time) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	ss, ok := s.db.sessions[id]
	if !ok || !bytes.Equal(ss.RefreshTokenHash, oldHash) {
		return auth.ErrSessionNotRotated
	}
	ss.PreviousRefreshTokenHash = oldHash
	ss.RefreshTokenHash = newHash
	ss.RotatedAt = &now
	s.db.sessions[id] = ss
	return nil
}

func (s fakeSessions) DeleteByID(_ context.Context, id uuid.UUID) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	delete(s.db.sessions, id)
	return nil
}

func (s fakeSessions) DeleteByUserID(_ context.Context, userID uuid.UUID) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	for id, ss := range s.db.sessions {
		if ss.UserID == userID {
			delete(s.db.sessions, id)
		}
	}
	return nil
}

func (s fakeSessions) DeleteExpired(_ context.Context, now time.Time, limit int32) (int64, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	var n int64
	for id, ss := range s.db.sessions {
		if n >= int64(limit) {
			break
		}
		if ss.IsExpired(now) {
			delete(s.db.sessions, id)
			n++
		}
	}
	return n, nil
}

// --- one-time user tokens ----------------------------------------------------

type fakeUserTokens struct{ db *fakeDB }

var _ auth.UserTokenStore = fakeUserTokens{}

func (s fakeUserTokens) Create(_ context.Context, t auth.UserToken) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	s.db.tokens = append(s.db.tokens, t)
	return nil
}

func (s fakeUserTokens) Consume(_ context.Context, hash []byte, purpose auth.TokenPurpose, now time.Time) (uuid.UUID, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	for i, t := range s.db.tokens {
		if !bytes.Equal(t.TokenHash, hash) || t.Purpose != purpose {
			continue
		}
		if t.UsedAt != nil || !now.Before(t.ExpiresAt) {
			return uuid.UUID{}, auth.ErrUserTokenInvalid
		}
		s.db.tokens[i].UsedAt = &now
		return t.UserID, nil
	}
	return uuid.UUID{}, auth.ErrUserTokenInvalid
}

func (s fakeUserTokens) DeleteByUserAndPurpose(_ context.Context, userID uuid.UUID, purpose auth.TokenPurpose) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	s.db.tokens = slices.DeleteFunc(s.db.tokens, func(t auth.UserToken) bool {
		return t.UserID == userID && t.Purpose == purpose
	})
	return nil
}

func (s fakeUserTokens) DeleteExpired(_ context.Context, now time.Time, limit int32) (int64, error) {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	var n int64
	s.db.tokens = slices.DeleteFunc(s.db.tokens, func(t auth.UserToken) bool {
		if n < int64(limit) && !now.Before(t.ExpiresAt) {
			n++
			return true
		}
		return false
	})
	return n, nil
}

// --- outbox ------------------------------------------------------------------

type fakeOutbox struct{ db *fakeDB }

var _ auth.OutboxWriter = fakeOutbox{}

func (s fakeOutbox) Create(_ context.Context, item email.OutboxItem) error {
	s.db.mu.Lock()
	defer s.db.mu.Unlock()
	s.db.outbox = append(s.db.outbox, item)
	return nil
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
