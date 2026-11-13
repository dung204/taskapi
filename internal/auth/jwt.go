package auth

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/golang-jwt/jwt/v5"
)

type jwtClaims struct {
	SessionID string `json:"sid"`
	jwt.RegisteredClaims
}

type JWTService struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	issuer     string
	audiences  []string
	ttl        time.Duration
}

type JWTServiceOpts struct {
	PrivateKey ed25519.PrivateKey
	PublicKey  ed25519.PublicKey
	Issuer     string
	Audiences  []string
	TTL        time.Duration
}

func NewJWTService(opts JWTServiceOpts) *JWTService {
	return &JWTService{
		privateKey: opts.PrivateKey,
		publicKey:  opts.PublicKey,
		issuer:     opts.Issuer,
		audiences:  opts.Audiences,
		ttl:        opts.TTL,
	}
}

// Issue implements [TokenIssuer].
func (s *JWTService) Issue(userID uuid.UUID, sessionID uuid.UUID, now time.Time) (string, time.Time, error) {
	expiresAt := now.Add(s.ttl)

	claims := jwtClaims{
		SessionID: sessionID.String(),
		Issuer:    s.issuer,
		Audience:  jwt.ClaimStrings(s.audiences),
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signedToken, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("issue jwt: %w", err)
	}

	return signedToken, expiresAt, nil
}

// Verify implements [TokenVerifier].
func (s *JWTService) Verify(tokenStr string, now time.Time) (Claims, error) {
	c := &jwtClaims{}

	token, err := jwt.ParseWithClaims(
		tokenStr, c,
		func(t *jwt.Token) (any, error) {
			return s.publicKey, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(s.issuer),
		jwt.WithAudience(s.audiences...),
		jwt.WithTimeFunc(func() time.Time {
			return now
		}),
		jwt.WithLeeway(5*time.Second),
		jwt.WithExpirationRequired(),
	)

	if err != nil {
		switch {
		case errors.Is(err, jwt.ErrTokenExpired):
			return Claims{}, ErrExpiredToken
		default:
			return Claims{}, ErrInvalidToken
		}
	}

	if !token.Valid {
		return Claims{}, ErrInvalidToken
	}

	userID, err := uuid.Parse(c.Subject)
	if err != nil || userID == uuid.Nil() {
		return Claims{}, ErrInvalidToken
	}

	sessionID, err := uuid.Parse(c.SessionID)
	if err != nil || sessionID == uuid.Nil() {
		return Claims{}, ErrInvalidToken
	}

	if c.IssuedAt == nil {
		return Claims{}, ErrInvalidToken
	}

	if c.ExpiresAt == nil {
		return Claims{}, ErrInvalidToken
	}

	return Claims{
		UserID:    userID,
		SessionID: sessionID,
		IssuedAt:  c.IssuedAt.UTC(),
	}, nil
}
