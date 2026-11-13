package auth

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/metrics"
)

type Tokens struct {
	AccessToken     string
	AccessExpiresIn time.Duration
	// Refresh is nil when [Service.Refresh] is handled in grace period (no refresh token rotation)
	// [Service.Login] & [Service.Register] always returns non-nil
	Refresh *RefreshToken
}

type RefreshToken struct {
	Value     string
	ExpiresAt time.Time
}

type Service struct {
	refreshTTL         time.Duration
	refreshGracePeriod time.Duration
	tx                 Transactor
	sessions           SessionStore
	userTokens         UserTokenStore
	users              UserStore
	outbox             OutboxWriter
	issuer             TokenIssuer
	hasher             Hasher
	dummyHash          string
	revoker            Revoker
	loginLimiter       LoginLimiter
	mailRequestLimiter MailRequestLimiter
	metrics            *metrics.Auth
}

type ServiceDeps struct {
	RefreshTTL         time.Duration
	RefreshGracePeriod time.Duration
	Tx                 Transactor
	Sessions           SessionStore
	UserTokens         UserTokenStore
	Users              UserStore
	Outbox             OutboxWriter
	Issuer             TokenIssuer
	Hasher             Hasher
	Revoker            Revoker
	LoginLimiter       LoginLimiter
	MailRequestLimiter MailRequestLimiter
	Metrics            *metrics.Auth
}

const (
	revokerTimeout = 250 * time.Millisecond
	limiterTimeout = 150 * time.Millisecond
)

func NewService(deps ServiceDeps) (*Service, error) {
	dummyPassword := "dummy-1"
	dummyHash, err := deps.Hasher.Hash(context.Background(), dummyPassword)
	if err != nil {
		return nil, fmt.Errorf("auth: hash dummy password: %w", err)
	}

	return &Service{
		refreshTTL:         deps.RefreshTTL,
		refreshGracePeriod: deps.RefreshGracePeriod,
		tx:                 deps.Tx,
		sessions:           deps.Sessions,
		userTokens:         deps.UserTokens,
		users:              deps.Users,
		outbox:             deps.Outbox,
		issuer:             deps.Issuer,
		hasher:             deps.Hasher,
		dummyHash:          dummyHash,
		revoker:            deps.Revoker,
		loginLimiter:       deps.LoginLimiter,
		mailRequestLimiter: deps.MailRequestLimiter,
		metrics:            deps.Metrics,
	}, nil
}

// Register creates an account and queues its verification mail. When retryAfter
// is positive the per-email mail limit was hit: nothing was created or queued,
// and the caller should answer 429.
func (s Service) Register(ctx context.Context, mail, password string, now time.Time) (retryAfter time.Duration, err error) {
	mailAddr, err := parseEmail(mail)
	if err != nil {
		return 0, err
	}

	pass, err := parsePassword(password)
	if err != nil {
		return 0, err
	}

	// Both outcomes below send a mail to this address, so both count against the
	// limit. It runs before the hash, which is the expensive part.
	rctx, cancel := context.WithTimeout(ctx, limiterTimeout)
	defer cancel()

	retryAfter, err = s.mailRequestLimiter.Allow(rctx, mailAddr)
	switch {
	case err != nil:
		return 0, err
	case retryAfter > 0:
		return retryAfter, nil
	}

	hashed, err := s.hasher.Hash(ctx, pass)
	if err != nil {
		return 0, err
	}

	u, err := NewUser(NewUserParams{Email: mailAddr, PasswordHash: &hashed}, now)
	if err != nil {
		return 0, err
	}

	item, err := email.NewOutboxItem(email.NewOutboxItemParams{
		UserID:  u.ID,
		Kind:    email.KindVerifyEmail,
		ToEmail: mailAddr,
	}, now)
	if err != nil {
		return 0, err
	}

	err = s.tx.WithTx(ctx, func(st Stores) error {
		if err := st.Users.Create(ctx, u); err != nil {
			return err
		}
		return st.Outbox.Create(ctx, item)
	})
	if errors.Is(err, ErrEmailTaken) {
		// Answer exactly like a new registration, so the response does not reveal
		// that the address has an account; the owner is told by mail instead.
		return 0, s.notifyAlreadyRegistered(ctx, mailAddr, now)
	}
	if err != nil {
		return 0, fmt.Errorf("register: %w", err)
	}

	return 0, nil
}

// notifyAlreadyRegistered queues the "you already have an account" mail. It runs
// outside the registration transaction, which Postgres has aborted after the
// unique violation.
func (s Service) notifyAlreadyRegistered(ctx context.Context, mailAddr string, now time.Time) error {
	existing, err := s.users.GetByEmail(ctx, mailAddr)
	if errors.Is(err, ErrUserNotFound) {
		return nil // deleted in between; nothing to tell anyone
	}
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	item, err := email.NewOutboxItem(email.NewOutboxItemParams{
		UserID:  existing.ID,
		Kind:    email.KindAlreadyRegistered,
		ToEmail: existing.Email,
	}, now)
	if err != nil {
		return fmt.Errorf("register: %w", err)
	}

	if err := s.outbox.Create(ctx, item); err != nil {
		return fmt.Errorf("register: %w", err)
	}
	return nil
}

func (s Service) Login(ctx context.Context, mail, password string, userAgent *string, now time.Time) (tokens Tokens, retryAfter time.Duration, err error) {
	defer func() { s.metrics.LoginsInc(loginResult(err)) }()

	mailAddr, err := parseEmail(mail)
	if err != nil {
		return Tokens{}, 0, err
	}

	pass, err := parseLoginPassword(password)
	if err != nil {
		return Tokens{}, 0, err
	}

	rctx, cancel := context.WithTimeout(ctx, limiterTimeout)
	defer cancel()

	wait, err := s.loginLimiter.Attempt(rctx, mailAddr)
	switch {
	case err != nil:
		return Tokens{}, 0, err
	case wait > 0:
		return Tokens{}, wait, apperr.ErrTooManyRequests
	}

	hashToVerify := s.dummyHash

	user, err := s.users.GetByEmail(ctx, mailAddr)
	if err != nil && !errors.Is(err, ErrUserNotFound) {
		return Tokens{}, 0, ErrInvalidCredentials
	}

	if user.PasswordHash != nil {
		hashToVerify = *user.PasswordHash
	}

	ok, err := s.hasher.Verify(ctx, pass, hashToVerify)
	switch {
	case err != nil:
		return Tokens{}, 0, err
	case !ok:
		return Tokens{}, 0, ErrInvalidCredentials
	}

	rctx, cancel = context.WithTimeout(ctx, limiterTimeout)
	defer cancel()

	if err := s.loginLimiter.Reset(rctx, mailAddr); err != nil {
		return Tokens{}, 0, err
	}

	switch {
	case user.EmailVerifiedAt == nil:
		return Tokens{}, 0, ErrEmailNotVerified
	case user.DisabledAt != nil:
		return Tokens{}, 0, ErrAccountDisabled
	}

	refresh, err := generateOpaqueToken()
	if err != nil {
		return Tokens{}, 0, err
	}

	hash := sha256.Sum256(refresh)
	session, err := NewSession(NewSessionParams{
		UserID:           user.ID,
		RefreshTokenHash: hash[:],
		UserAgent:        userAgent,
		TTL:              s.refreshTTL,
	}, now)
	if err != nil {
		return Tokens{}, 0, err
	}

	if err := s.sessions.Create(ctx, session); err != nil {
		return Tokens{}, 0, err
	}

	pair, err := s.issueTokenPair(session.UserID, session.ID, refresh, session.ExpiresAt, now)
	if err != nil {
		return Tokens{}, 0, err
	}

	return pair, 0, nil
}

func loginResult(err error) string {
	switch {
	case err == nil:
		return metrics.LoginSuccess
	case errors.Is(err, ErrInvalidCredentials):
		return metrics.LoginInvalidCredentials
	case errors.Is(err, apperr.ErrTooManyRequests):
		return metrics.LoginThrottled
	case errors.Is(err, ErrEmailNotVerified):
		return metrics.LoginEmailNotVerified
	case errors.Is(err, ErrAccountDisabled):
		return metrics.LoginAccountDisabled
	case errors.Is(err, apperr.ErrInvalidInput):
		return metrics.LoginInvalidRequest
	default:
		return metrics.LoginError
	}
}

func (s Service) Refresh(ctx context.Context, refreshToken string, now time.Time) (Tokens, error) {
	raw, err := decodeOpaqueToken(refreshToken)
	if errors.Is(err, ErrInvalidToken) {
		return Tokens{}, ErrInvalidRefreshTokenNeedLogout
	}

	if err != nil {
		return Tokens{}, err
	}

	hash := sha256.Sum256(raw)
	oldHash := hash[:]
	session, err := s.sessions.GetByRefreshTokenHash(ctx, oldHash)

	switch {
	case errors.Is(err, ErrSessionNotFound):
		return Tokens{}, ErrInvalidRefreshToken
	case err != nil:
		return Tokens{}, err
	case session.IsExpired(now):
		return Tokens{}, ErrInvalidRefreshTokenNeedLogout
	}

	match := session.MatchRefresh(oldHash, now, s.refreshGracePeriod)
	if match == RefreshReuse {
		// An old token outside the grace period: assume theft and kill the session,
		// including the newest token.
		if err := s.sessions.DeleteByID(ctx, session.ID); err != nil {
			return Tokens{}, err
		}

		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokerTimeout)
		defer cancel()
		if err := s.revoker.RevokeSession(rctx, session.ID); err != nil {
			return Tokens{}, err
		}

		return Tokens{}, ErrInvalidRefreshTokenNeedLogout
	}

	user, err := s.users.GetByID(ctx, session.UserID)
	switch {
	case errors.Is(err, ErrUserNotFound):
		return Tokens{}, ErrInvalidRefreshTokenNeedLogout
	case err != nil:
		return Tokens{}, err
	case user.DisabledAt != nil:
		return Tokens{}, ErrAccountDisabled
	}

	if match == RefreshGrace {
		access, accessExp, err := s.issuer.Issue(session.UserID, session.ID, now)
		if err != nil {
			return Tokens{}, err
		}

		return Tokens{
			AccessToken:     access,
			AccessExpiresIn: accessExp.Sub(now),
		}, nil
	}

	newRaw, err := generateOpaqueToken()
	if err != nil {
		return Tokens{}, err
	}

	newHash := sha256.Sum256(newRaw)
	err = s.sessions.Rotate(ctx, session.ID, oldHash, newHash[:], now)
	switch {
	case errors.Is(err, ErrSessionNotRotated):
		// Another request rotated this session first (two tabs at once).
		return Tokens{}, ErrInvalidRefreshToken
	case err != nil:
		return Tokens{}, err
	}

	return s.issueTokenPair(session.UserID, session.ID, newRaw, session.ExpiresAt, now)
}

func (s Service) issueTokenPair(userID, sessionID uuid.UUID, refresh []byte, refreshExpiresAt, now time.Time) (Tokens, error) {
	access, accessExp, err := s.issuer.Issue(userID, sessionID, now)
	if err != nil {
		return Tokens{}, err
	}

	return Tokens{
		AccessToken:     access,
		AccessExpiresIn: accessExp.Sub(now),
		Refresh: &RefreshToken{
			Value:     base64.RawURLEncoding.EncodeToString(refresh),
			ExpiresAt: refreshExpiresAt,
		},
	}, nil
}

func (s Service) Logout(ctx context.Context, refreshToken string, now time.Time) error {
	raw, err := decodeOpaqueToken(refreshToken)
	if err != nil {
		return nil
	}

	hash := sha256.Sum256(raw)
	session, err := s.sessions.GetByRefreshTokenHash(ctx, hash[:])
	switch {
	case errors.Is(err, ErrSessionNotFound):
		return nil
	case err != nil:
		return err
	}

	if err := s.sessions.DeleteByID(ctx, session.ID); err != nil {
		return err
	}

	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokerTimeout)
	defer cancel()

	return s.revoker.RevokeSession(rctx, session.ID)
}

func (s Service) LogoutAll(ctx context.Context, userID uuid.UUID, now time.Time) error {
	if err := s.tx.WithTx(ctx, func(st Stores) error {
		if err := st.Users.SetTokensValidAfter(ctx, userID, now); err != nil {
			return err
		}
		return s.sessions.DeleteByUserID(ctx, userID)
	}); err != nil {
		return err
	}

	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokerTimeout)
	defer cancel()

	return s.revoker.RevokeUser(rctx, userID, now)
}

func (s Service) SendVerificationMail(ctx context.Context, mail string, now time.Time) (retryAfter time.Duration, err error) {
	mailAddr, err := parseEmail(mail)
	if err != nil {
		return 0, err
	}

	rctx, cancel := context.WithTimeout(ctx, limiterTimeout)
	defer cancel()

	retryAfter, err = s.mailRequestLimiter.Allow(rctx, mailAddr)
	switch {
	case err != nil:
		return 0, err
	case retryAfter > 0:
		return retryAfter, nil
	}

	user, err := s.users.GetByEmail(ctx, mailAddr)
	if errors.Is(err, ErrUserNotFound) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	if user.EmailVerifiedAt != nil {
		return 0, nil
	}

	item, err := email.NewOutboxItem(email.NewOutboxItemParams{
		UserID:  user.ID,
		Kind:    email.KindVerifyEmail,
		ToEmail: user.Email,
	}, now)
	if err != nil {
		return 0, err
	}

	return 0, s.outbox.Create(ctx, item)
}

func (s Service) VerifyEmail(ctx context.Context, token string, now time.Time) error {
	raw, err := decodeOpaqueToken(token)
	if err != nil {
		return err
	}

	hash := sha256.Sum256(raw)

	return s.tx.WithTx(ctx, func(s Stores) error {
		userID, err := s.UserTokens.Consume(ctx, hash[:], PurposeVerifyEmail, now)
		if err != nil {
			return err
		}

		return s.Users.SetEmailVerified(ctx, userID, now)
	})
}

func (s Service) SendForgotPasswordMail(ctx context.Context, mail string, now time.Time) (retryAfter time.Duration, err error) {
	mailAddr, err := parseEmail(mail)
	if err != nil {
		return 0, err
	}

	rctx, cancel := context.WithTimeout(ctx, limiterTimeout)
	defer cancel()

	retryAfter, err = s.mailRequestLimiter.Allow(rctx, mailAddr)
	switch {
	case err != nil:
		return 0, err
	case retryAfter > 0:
		return retryAfter, nil
	}

	user, err := s.users.GetByEmail(ctx, mailAddr)
	if errors.Is(err, ErrUserNotFound) {
		return 0, nil
	}

	if err != nil {
		return 0, err
	}

	item, err := email.NewOutboxItem(email.NewOutboxItemParams{
		UserID:  user.ID,
		Kind:    email.KindResetPassword,
		ToEmail: user.Email,
	}, now)
	if err != nil {
		return 0, err
	}

	return 0, s.outbox.Create(ctx, item)
}

func (s Service) ResetPassword(ctx context.Context, token, password string, now time.Time) error {
	raw, err := decodeOpaqueToken(token)
	if err != nil {
		return err
	}

	pass, err := parsePassword(password)
	if err != nil {
		return err
	}

	hash, err := s.hasher.Hash(ctx, pass)
	if err != nil {
		return err
	}

	tokenHash := sha256.Sum256(raw)

	var userID uuid.UUID

	if err := s.tx.WithTx(ctx, func(st Stores) error {
		var err error
		userID, err = st.UserTokens.Consume(ctx, tokenHash[:], PurposeResetPassword, now)
		if err != nil {
			return err
		}

		if err := st.Users.UpdatePassword(ctx, userID, hash, now); err != nil {
			return err
		}

		// Whoever knew the old password (or had a session) must sign in again.
		return st.Sessions.DeleteByUserID(ctx, userID)
	}); err != nil {
		return err
	}

	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), revokerTimeout)
	defer cancel()

	return s.revoker.RevokeUser(rctx, userID, now)
}

func (s Service) GetMe(ctx context.Context, userID uuid.UUID) (User, error) {
	user, err := s.users.GetByID(ctx, userID)
	switch {
	case err != nil:
		return User{}, err
	case user.DisabledAt != nil:
		return User{}, ErrAccountDisabled
	default:
		return user, nil
	}
}

// ChangePassword sets a new password. Wrong current passwords are limited with
// the same per-email limiter as logins, so the two share one budget: when the
// email has to wait, retryAfter is set and the error is
// apperr.ErrTooManyRequests, even if the current password is right. A success
// resets the counter.
func (s Service) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string, now time.Time) (retryAfter time.Duration, err error) {
	currentPass, err := parseLoginPassword(currentPassword)
	if err != nil {
		return 0, err
	}

	newPass, err := parsePassword(newPassword)
	if err != nil {
		return 0, err
	}

	user, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return 0, err
	}

	rctx, cancel := context.WithTimeout(ctx, limiterTimeout)
	defer cancel()

	wait, err := s.loginLimiter.Attempt(rctx, user.Email)
	switch {
	case err != nil:
		return 0, err
	case wait > 0:
		return wait, apperr.ErrTooManyRequests
	}

	ok, err := s.hasher.Verify(ctx, currentPass, *cmp.Or(user.PasswordHash, &s.dummyHash))
	switch {
	case err != nil:
		return 0, err
	case !ok:
		return 0, ErrWrongCurrentPassword
	}

	newPassHash, err := s.hasher.Hash(ctx, newPass)
	if err != nil {
		return 0, err
	}

	if err := s.tx.WithTx(ctx, func(st Stores) error {
		if err := st.Users.UpdatePassword(ctx, user.ID, newPassHash, now); err != nil {
			return err
		}
		return st.Sessions.DeleteByUserID(ctx, user.ID)
	}); err != nil {
		return 0, err
	}

	rctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), revokerTimeout)
	defer cancel()

	if err := s.revoker.RevokeUser(rctx, userID, now); err != nil {
		return 0, err
	}

	if err := s.loginLimiter.Reset(rctx, user.Email); err != nil {
		return 0, err
	}
	return 0, nil
}
