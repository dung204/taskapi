package oapigen

import (
	"fmt"
	"time"

	"github.com/dung204/taskapi/internal/task"
)

func ToAPITask(t task.Task) Task {
	return Task{
		Id:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		DueAt:       t.DueAt,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

func ToAPITasks(ts []task.Task) []Task {
	apiTasks := make([]Task, len(ts))
	for i, t := range ts {
		apiTasks[i] = ToAPITask(t)
	}
	return apiTasks
}

func parseTime(field string, s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be an RFC3339 date-time, for example 2030-01-01T00:00:00Z", task.ErrInvalidInput, field)
	}

	return &t, nil
}

func (r *CreateTaskRequest) ToNewParams() (task.NewParams, error) {
	dueAt, err := parseTime("due_at", r.DueAt)
	if err != nil {
		return task.NewParams{}, err
	}

	var status Status
	if r.Status == nil {
		status = task.StatusTodo
	} else {
		status = *r.Status
	}

	return task.NewParams{
		Title:       r.Title,
		Description: r.Description,
		Status:      status,
		DueAt:       dueAt,
	}, nil
}

func (r *UpdateTaskRequest) ToPatch() (task.Patch, error) {
	dueAt, err := parseTime("due_at", r.DueAt)
	if err != nil {
		return task.Patch{}, err
	}

	return task.Patch{
		Title:       r.Title,
		Description: r.Description,
		Status:      r.Status,
		DueAt:       dueAt,
	}, nil
}

const (
	defaultLimit  = 20
	defaultOffset = 0
	maxLimit      = 100
)

func (p ListTasksParams) ToListFilter() (task.ListFilter, error) {
	limit := defaultLimit
	if p.Limit != nil {
		if *p.Limit < 1 {
			return task.ListFilter{}, fmt.Errorf("%w: limit is invalid", task.ErrInvalidInput)
		}
		limit = min(*p.Limit, maxLimit)
	}

	offset := defaultOffset
	if p.Offset != nil {
		if *p.Offset < 0 {
			return task.ListFilter{}, fmt.Errorf("%w: offset is invalid", task.ErrInvalidInput)
		}
		offset = *p.Offset
	}

	var status task.Status
	if p.Status != nil && *p.Status != "" {
		if !p.Status.IsValidForGet() {
			return task.ListFilter{}, task.ErrInvalidGetStatus
		}
		status = *p.Status
	}

	return task.ListFilter{
		Limit:  limit,
		Offset: offset,
		Status: status,
	}, nil
}
