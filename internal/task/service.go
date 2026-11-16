package task

import (
	"time"
	"uuid"
)

type Service struct {
	tasks Store
}

type ServiceDeps struct {
	Tasks Store
}

func NewService(deps ServiceDeps) *Service {
	return &Service{
		deps.Tasks,
	}
}

func (s Service) Create(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}
