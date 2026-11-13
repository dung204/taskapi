package email

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dung204/taskapi/internal/apperr"
)

var (
	ErrInvalidKind   = fmt.Errorf("%w: kind must be one of the following: %s", apperr.ErrInvalidInput, strings.Join(allowedKinds, ", "))
	ErrNothingToSend = errors.New("email: nothing to send")
)
