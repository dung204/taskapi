// Package apperr holds the sentinel errors shared across packages. The HTTP
// layer maps them to response statuses.
package apperr

import "errors"

var (
	ErrInvalidInput        = errors.New("invalid input")
	ErrNotFound            = errors.New("not found")
	ErrServiceUnavailable  = errors.New("service unavailable")
	ErrForbidden           = errors.New("forbidden")
	ErrInternalServerError = errors.New("internal server error")
	ErrUnauthorized        = errors.New("unauthorized")
	ErrTooManyRequests     = errors.New("too many requests")
	ErrConflict            = errors.New("conflict")
	ErrUnprocessable       = errors.New("unprocessable")
)
