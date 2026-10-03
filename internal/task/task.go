package task

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
	"uuid"
)

type Status string

const (
	StatusTodo    Status = "todo"
	StatusDoing   Status = "doing"
	StatusDone    Status = "done"
	StatusOverdue Status = "overdue"
)

var allowedPatchStatuses = []string{
	string(StatusTodo),
	string(StatusDoing),
	string(StatusDone),
}

var allowedGetStatuses = append(allowedPatchStatuses, string(StatusOverdue))

func (s Status) IsValidForPatch() bool {
	return slices.Contains(allowedPatchStatuses, string(s))
}

func (s Status) IsValidForGet() bool {
	return slices.Contains(allowedGetStatuses, string(s))
}

type Task struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      Status     `json:"status"`
	DueAt       *time.Time `json:"due_at"` // nullable
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type NewParams struct {
	Title       string
	Description *string
	Status      Status
	DueAt       *time.Time
}

func New(params NewParams) (*Task, error) {
	id := uuid.NewV7()
	now := time.Now().UTC().Truncate(time.Microsecond)
	status := cmp.Or(params.Status, StatusTodo)

	t := &Task{
		ID:        id,
		CreatedAt: now,
	}

	err := t.Patch(Patch{
		Title:       &params.Title,
		Description: params.Description,
		Status:      &status,
		DueAt:       params.DueAt,
	})

	if err != nil {
		return nil, err
	}

	// Since this is create new instance of task -> CreatedAt = UpdatedAt
	t.UpdatedAt = now

	return t, nil
}

func (t *Task) Patch(p Patch) error {
	changeUpdatedAt := false

	title := p.Title
	if title != nil {
		trimmed := strings.TrimSpace(*title)
		if utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > 200 {
			return fmt.Errorf("%w: title must have at least 1 and at most 200 characters", ErrInvalidInput)
		}

		t.Title = trimmed
		changeUpdatedAt = true
	}

	description := p.Description
	if description != nil {
		if utf8.RuneCountInString(*description) > 2000 {
			return fmt.Errorf("%w: description must have at most 2000 characters", ErrInvalidInput)
		}

		t.Description = *description
		changeUpdatedAt = true
	}

	status := p.Status
	if status != nil {
		if !status.IsValidForPatch() {
			return ErrInvalidPatchStatus
		}

		t.Status = *status
		changeUpdatedAt = true
	}

	if p.DueAt != nil && (t.DueAt == nil || p.DueAt.Compare(*t.DueAt) != 0) {
		truncated := p.DueAt.UTC().Truncate(time.Microsecond)
		t.DueAt = &truncated
		changeUpdatedAt = true
	}

	if changeUpdatedAt {
		t.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	}

	return nil
}
