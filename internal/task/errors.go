package task

import (
	"fmt"
	"strings"

	"github.com/dung204/taskapi/internal/apperr"
)

var (
	ErrNotFound = fmt.Errorf("task: %w", apperr.ErrNotFound)

	ErrInvalidGetStatus   = fmt.Errorf("%w: status must be one of the following: %s", apperr.ErrInvalidInput, strings.Join(allowedGetStatuses, ", "))
	ErrInvalidPatchStatus = fmt.Errorf("%w: status must be one of the following: %s", apperr.ErrInvalidInput, strings.Join(allowedPatchStatuses, ", "))
)
