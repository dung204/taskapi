package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
)

// The three handlers below are plugged into the generated code by NewHandler:
// one for parameters that fail to bind, one for request bodies that fail to
// decode, and one for the errors returned by the handler methods.

func (s *server) handleParamError(w http.ResponseWriter, _ *http.Request, err error) {
	var errInvalid *oapigen.InvalidParamFormatError
	var errRequired *oapigen.RequiredParamError

	switch {
	case errors.As(err, &errInvalid):
		writeError(w, http.StatusBadRequest, "invalid "+errInvalid.ParamName)
	case errors.As(err, &errRequired):
		writeError(w, http.StatusBadRequest, errRequired.ParamName+" is required")
	default:
		writeError(w, http.StatusBadRequest, "invalid request")
	}
}

func (s *server) handleRequestError(w http.ResponseWriter, _ *http.Request, err error) {
	var (
		msg       string
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)

	switch {
	case errors.Is(err, io.EOF):
		msg = "request body is required"
	case errors.Is(err, io.ErrUnexpectedEOF):
		msg = "request body contains malformed JSON"
	case errors.As(err, &syntaxErr):
		msg = fmt.Sprintf("request body contains malformed JSON (at position %d)", syntaxErr.Offset)
	case errors.As(err, &typeErr):
		msg = fmt.Sprintf(`field "%s" has the wrong type`, typeErr.Field)
		if typeErr.Field == "" {
			msg = "request body must be a JSON object"
		}
	default:
		msg = "invalid request body"
	}

	writeError(w, http.StatusBadRequest, msg)
}

func (s *server) handleResponseError(l *slog.Logger) func(w http.ResponseWriter, r *http.Request, err error) {
	return func(w http.ResponseWriter, r *http.Request, err error) {
		ctx := r.Context()

		switch {
		case errors.Is(err, apperr.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())

		case errors.Is(err, apperr.ErrUnauthorized):
			writeError(w, http.StatusUnauthorized, err.Error())

		case errors.Is(err, apperr.ErrForbidden):
			writeError(w, http.StatusForbidden, err.Error())

		case errors.Is(err, apperr.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())

		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusServiceUnavailable, apperr.ErrServiceUnavailable.Error())

		case errors.Is(err, apperr.ErrServiceUnavailable):
			writeError(w, http.StatusServiceUnavailable, apperr.ErrServiceUnavailable.Error())
			l.ErrorContext(ctx, "unexpected error", "error", err)

		case errors.Is(err, context.Canceled):
			// the client cancelled the request, no need to handle here

		case err != nil:
			writeError(w, http.StatusInternalServerError, apperr.ErrInternalServerError.Error())
			l.ErrorContext(ctx, "unexpected error", "error", err)
		}
	}
}
