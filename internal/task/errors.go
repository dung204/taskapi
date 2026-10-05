package task

import (
	"errors"
	"fmt"
	"strings"
)

var ErrNotFound = errors.New("task not found")
var ErrInvalidInput = errors.New("invalid input")
var ErrInvalidGetStatus = fmt.Errorf("%w: status must be one of the following: %s", ErrInvalidInput, strings.Join(allowedGetStatuses, ", "))
var ErrInvalidPatchStatus = fmt.Errorf("%w: status must be one of the following: %s", ErrInvalidInput, strings.Join(allowedPatchStatuses, ", "))
var ErrServiceUnavailable = errors.New("service unavailable")
