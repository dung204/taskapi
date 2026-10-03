package task

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"uuid"
)

type Status string

const (
	StatusTodo    Status = Status("todo")
	StatusDoing   Status = Status("doing")
	StatusDone    Status = Status("done")
	StatusOverdue Status = Status("overdue")
)

var AllowedStatuses = []string{
	string(StatusTodo),
	string(StatusDoing),
	string(StatusDone),
	string(StatusOverdue),
}

type Task struct {
	ID          uuid.UUID
	Title       string
	Description string
	Status      Status
	DueAt       *time.Time // nullable
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type NewParams struct {
	Title       string
	Description string
	Status      Status
	DueAt       *time.Time
}

func New(params NewParams) (*Task, error) {
	title := strings.TrimSpace(params.Title)
	if len(title) < 1 || len(title) > 200 {
		return nil, fmt.Errorf("%w: title must have at least 1 and at most 200 characters", ErrInvalidInput)
	}

	description := params.Description
	if len(description) > 2000 {
		return nil, fmt.Errorf("%w: description must have at most 2000 characters", ErrInvalidInput)
	}

	status := cmp.Or(params.Status, StatusTodo)
	if !slices.Contains(AllowedStatuses, string(status)) {
		return nil, fmt.Errorf("%w: status must be one of the following: %s", ErrInvalidInput, strings.Join(AllowedStatuses, ", "))
	}

	if status == StatusOverdue {
		return nil, fmt.Errorf("%w: new task can not have the status of %s", ErrInvalidInput, StatusOverdue)
	}

	id := uuid.NewV7()
	dueAt := params.DueAt
	now := time.Now().UTC().Truncate(time.Microsecond)

	return &Task{
		ID:          id,
		Title:       title,
		Description: description,
		Status:      status,
		DueAt:       dueAt,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}
