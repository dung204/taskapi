package httpapi

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
)

type authHandler struct {
	service *auth.Service
	origins *OriginPolicy
	logger  *slog.Logger
	now     func() time.Time
}

func newAuthHandler(s *auth.Service, p *OriginPolicy, l *slog.Logger, now func() time.Time) *authHandler {
	if now == nil {
		now = time.Now
	}

	return &authHandler{
		service: s,
		origins: p,
		logger:  l,
		now:     now,
	}
}

// ForgotPassword Ask for a password reset email
// (POST /auth/forgot-password)
func (h *authHandler) ForgotPassword(ctx context.Context, request oapigen.ForgotPasswordRequestObject) (oapigen.ForgotPasswordResponseObject, error) {
	retryAfter, err := h.service.SendForgotPasswordMail(ctx, request.Body.Email, h.now())
	switch {
	case retryAfter > 0:
		return oapigen.ForgotPassword429JSONResponse{
			Headers: oapigen.ForgotPassword429ResponseHeaders{
				RetryAfter: max(1, int64(math.Ceil(retryAfter.Seconds()))),
			},
			Body: oapigen.Error{
				Error: apperr.ErrTooManyRequests.Error(),
			},
		}, nil
	case err != nil:
		return nil, err
	}

	return oapigen.ForgotPassword202Response{}, nil
}

// Login Sign in
// (POST /auth/login)
func (h *authHandler) Login(ctx context.Context, request oapigen.LoginRequestObject) (oapigen.LoginResponseObject, error) {
	tokenMode := *cmp.Or(request.Body.TokenMode, new(oapigen.Token))
	if !tokenMode.IsValid() {
		return oapigen.Login400JSONResponse{Error: auth.ErrInvalidTokenMode.Error()}, nil
	}

	tokens, wait, err := h.service.Login(ctx, request.Body.Email, request.Body.Password, new(userAgentFrom(ctx)), h.now())

	switch {
	case wait > 0:
		return oapigen.Login429JSONResponse{
			Headers: oapigen.Login429ResponseHeaders{
				RetryAfter: max(1, int64(math.Ceil(wait.Seconds()))),
			},
			Body: oapigen.Error{
				Error: apperr.ErrTooManyRequests.Error(),
			},
		}, nil
	case errors.Is(err, auth.ErrInvalidCredentials):
		return oapigen.Login401JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, auth.ErrEmailNotVerified) || errors.Is(err, auth.ErrAccountDisabled):
		return oapigen.Login403JSONResponse{Error: err.Error()}, nil
	case err != nil:
		return nil, err
	}

	switch tokenMode {
	case oapigen.Cookie:
		cookie := refreshTokenCookieWith(refreshTokenCookieWithParams{
			Value:   tokens.Refresh.Value,
			Expires: tokens.Refresh.ExpiresAt,
		})

		return oapigen.Login200JSONResponse{
			Body: oapigen.TokenResponse{
				AccessToken: tokens.AccessToken,
				TokenType:   oapigen.Bearer,
				ExpiresIn:   int64(tokens.AccessExpiresIn.Seconds()),
			},
			Headers: oapigen.Login200ResponseHeaders{
				SetCookie: new(cookie.String()),
			},
		}, nil
	case oapigen.Token:
		return oapigen.Login200JSONResponse{
			Body: oapigen.TokenResponse{
				AccessToken:  tokens.AccessToken,
				TokenType:    oapigen.Bearer,
				ExpiresIn:    int64(tokens.AccessExpiresIn.Seconds()),
				RefreshToken: &tokens.Refresh.Value,
			},
		}, nil
	}

	return nil, err
}

// Logout Sign out this session
// (POST /auth/logout)
func (h *authHandler) Logout(ctx context.Context, request oapigen.LogoutRequestObject) (oapigen.LogoutResponseObject, error) {
	refreshToken, _, err := oapigen.ParseRefreshToken(request.Params.RefreshToken, request.Params.Origin, request.Body, h.origins.Allows)
	if err != nil {
		return nil, err
	}

	if err := h.service.Logout(ctx, refreshToken, h.now()); err != nil {
		return nil, err
	}

	return oapigen.Logout204Response{
		Headers: oapigen.Logout204ResponseHeaders{
			SetCookie: new(clearedCookie.String()),
		},
	}, nil
}

// LogoutAll Sign out every session of the user
// (POST /auth/logout-all)
func (h *authHandler) LogoutAll(ctx context.Context, request oapigen.LogoutAllRequestObject) (oapigen.LogoutAllResponseObject, error) {
	err := h.service.LogoutAll(ctx, identityFrom(ctx).UserID, h.now())
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return nil, apperr.ErrUnauthorized
	case err != nil:
		return nil, err
	default:
		return oapigen.LogoutAll204Response{
			Headers: oapigen.LogoutAll204ResponseHeaders{
				SetCookie: new(clearedCookie.String()),
			},
		}, nil
	}
}

// RefreshToken Get a new access token
// (POST /auth/refresh)
func (h *authHandler) RefreshToken(ctx context.Context, request oapigen.RefreshTokenRequestObject) (oapigen.RefreshTokenResponseObject, error) {
	refreshToken, fromCookie, err := oapigen.ParseRefreshToken(request.Params.RefreshToken, request.Params.Origin, request.Body, h.origins.Allows)
	if err != nil {
		return nil, err
	}

	tokens, err := h.service.Refresh(ctx, refreshToken, h.now())
	switch {
	case errors.Is(err, auth.ErrInvalidRefreshTokenNeedLogout):
		var cookie *string
		if fromCookie {
			cookie = new(clearedCookie.String())
		}

		return oapigen.RefreshToken401JSONResponse{
			Body: oapigen.Error{
				Error: apperr.ErrUnauthorized.Error(),
			},
			Headers: oapigen.RefreshToken401ResponseHeaders{
				SetCookie: cookie,
			},
		}, nil

	case errors.Is(err, auth.ErrInvalidRefreshToken):
		return nil, apperr.ErrUnauthorized

	case errors.Is(err, auth.ErrAccountDisabled):
		return oapigen.RefreshToken403JSONResponse{Error: err.Error()}, nil

	case err != nil:
		return nil, err
	}

	res := oapigen.RefreshToken200JSONResponse{
		Body: oapigen.TokenResponse{
			AccessToken: tokens.AccessToken,
			TokenType:   oapigen.Bearer,
			ExpiresIn:   int64(tokens.AccessExpiresIn.Seconds()),
		},
	}

	// A grace-period answer has no refresh token: the client keeps the one it
	// got from the request that rotated the session.
	if tokens.Refresh != nil {
		if fromCookie {
			cookie := refreshTokenCookieWith(refreshTokenCookieWithParams{
				Value:   tokens.Refresh.Value,
				Expires: tokens.Refresh.ExpiresAt,
			})
			res.Headers.SetCookie = new(cookie.String())
		} else {
			res.Body.RefreshToken = &tokens.Refresh.Value
		}
	}

	return res, nil
}

// Register Register an account
// (POST /auth/register)
func (h *authHandler) Register(ctx context.Context, request oapigen.RegisterRequestObject) (oapigen.RegisterResponseObject, error) {
	retryAfter, err := h.service.Register(ctx, request.Body.Email, request.Body.Password, h.now())
	switch {
	case retryAfter > 0:
		return oapigen.Register429JSONResponse{
			Headers: oapigen.Register429ResponseHeaders{
				RetryAfter: max(1, int64(math.Ceil(retryAfter.Seconds()))),
			},
			Body: oapigen.Error{
				Error: apperr.ErrTooManyRequests.Error(),
			},
		}, nil
	case err != nil:
		return nil, err
	}

	return oapigen.Register202Response{}, nil
}

// ResendVerification Send the verification email again
// (POST /auth/resend-verification)
func (h *authHandler) ResendVerification(ctx context.Context, request oapigen.ResendVerificationRequestObject) (oapigen.ResendVerificationResponseObject, error) {
	retryAfter, err := h.service.SendVerificationMail(ctx, request.Body.Email, h.now())
	switch {
	case retryAfter > 0:
		return oapigen.ResendVerification429JSONResponse{
			Headers: oapigen.ResendVerification429ResponseHeaders{
				RetryAfter: max(1, int64(math.Ceil(retryAfter.Seconds()))),
			},
			Body: oapigen.Error{
				Error: apperr.ErrTooManyRequests.Error(),
			},
		}, nil
	case err != nil:
		return nil, err
	}

	return oapigen.ResendVerification202Response{}, nil
}

// ResetPassword Set a new password with a reset token
// (POST /auth/reset-password)
func (h *authHandler) ResetPassword(ctx context.Context, request oapigen.ResetPasswordRequestObject) (oapigen.ResetPasswordResponseObject, error) {
	err := h.service.ResetPassword(ctx, request.Body.Token, request.Body.NewPassword, h.now())
	switch {
	case isOneTimeTokenError(err):
		return oapigen.ResetPassword400JSONResponse{Error: auth.ErrInvalidToken.Error()}, nil
	case err != nil:
		return nil, err
	}

	return oapigen.ResetPassword204Response{}, nil
}

// VerifyEmail Verify an email address
// (POST /auth/verify-email)
func (h *authHandler) VerifyEmail(ctx context.Context, request oapigen.VerifyEmailRequestObject) (oapigen.VerifyEmailResponseObject, error) {
	err := h.service.VerifyEmail(ctx, request.Body.Token, h.now())
	switch {
	case isOneTimeTokenError(err):
		return oapigen.VerifyEmail400JSONResponse{Error: auth.ErrInvalidToken.Error()}, nil
	case err != nil:
		return nil, err
	}

	return oapigen.VerifyEmail204Response{}, nil
}

// GetMe Get the signed-in user
// (GET /me)
func (h *authHandler) GetMe(ctx context.Context, request oapigen.GetMeRequestObject) (oapigen.GetMeResponseObject, error) {
	user, err := h.service.GetMe(ctx, identityFrom(ctx).UserID)
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return nil, apperr.ErrUnauthorized
	case errors.Is(err, auth.ErrAccountDisabled):
		return oapigen.GetMe403JSONResponse{Error: err.Error()}, nil
	case err != nil:
		return nil, err
	default:
		return oapigen.GetMe200JSONResponse(oapigen.ToAPIUser(user)), nil
	}
}

// ChangePassword Change the password
// (PUT /me/password)
func (h *authHandler) ChangePassword(ctx context.Context, request oapigen.ChangePasswordRequestObject) (oapigen.ChangePasswordResponseObject, error) {
	wait, err := h.service.ChangePassword(ctx, identityFrom(ctx).UserID, request.Body.CurrentPassword, request.Body.NewPassword, h.now())
	switch {
	case wait > 0:
		return oapigen.ChangePassword429JSONResponse{
			Headers: oapigen.ChangePassword429ResponseHeaders{
				RetryAfter: max(1, int64(math.Ceil(wait.Seconds()))),
			},
			Body: oapigen.Error{
				Error: apperr.ErrTooManyRequests.Error(),
			},
		}, nil
	case errors.Is(err, auth.ErrWrongCurrentPassword):
		return oapigen.ChangePassword403JSONResponse{Error: err.Error()}, nil
	case err != nil:
		return nil, err
	default:
		return oapigen.ChangePassword204Response{}, nil
	}
}

// isOneTimeTokenError reports whether err means the emailed token cannot be
// used: malformed, unknown, expired, used already, or for another purpose. They
// all get one message, so a client cannot tell which one it was.
func isOneTimeTokenError(err error) bool {
	return errors.Is(err, auth.ErrInvalidToken) ||
		errors.Is(err, auth.ErrExpiredToken) ||
		errors.Is(err, auth.ErrUserTokenInvalid)
}

func baseRefreshTokenCookie() *http.Cookie {
	return &http.Cookie{
		Name:     "refresh_token",
		Path:     "/auth",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
}

type refreshTokenCookieWithParams struct {
	Value   string
	Expires time.Time
	MaxAge  int
}

func refreshTokenCookieWith(p refreshTokenCookieWithParams) *http.Cookie {
	c := baseRefreshTokenCookie()
	c.Value = p.Value
	c.Expires = p.Expires
	c.MaxAge = p.MaxAge

	return c
}

var clearedCookie = refreshTokenCookieWith(refreshTokenCookieWithParams{
	Value:   "",
	MaxAge:  -1,
	Expires: time.Unix(0, 0),
})
