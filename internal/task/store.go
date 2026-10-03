package task

import (
	"context"
	"time"
	"uuid"
)

type ListFilter struct {
	Limit  int
	Offset int
	Status Status
}

type Patch struct {
	Title       *string
	Description *string
	Status      *Status
	DueAt       *time.Time
}

type Store interface {
	Create(ctx context.Context, t Task) (Task, error)
	Get(ctx context.Context, id uuid.UUID) (Task, error)
	List(ctx context.Context, f ListFilter) ([]Task, error)
	Update(ctx context.Context, id uuid.UUID, p Patch) (Task, error)
	Delete(ctx context.Context, id uuid.UUID) error
}
