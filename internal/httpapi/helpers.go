package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/dung204/taskapi/internal/task"
)

func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	b, err := json.Marshal(v)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("{\"error\":\"internal server error\"}"))
		fmt.Fprintln(os.Stderr, err) // TODO: replace with logger
		return err
	}

	w.WriteHeader(status)
	w.Write(b)
	return nil
}

func writeError(w http.ResponseWriter, status int, msg string) error {
	return writeJSON(w, status, map[string]string{"error": msg})
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	err := json.NewDecoder(r.Body).Decode(dst)

	if err != nil {
		var msg string
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError

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
		return err
	}

	return nil
}

func parseTime(field string, s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be an RFC 3339 date-time, for example 2030-01-01T00:00:00Z", task.ErrInvalidInput, field)
	}

	return &t, nil
}
