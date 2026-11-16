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

func (s Service) Get(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) List(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) Update(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) Delete(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) Assign(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) Unassign(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) ListShares(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) Invite(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) CancelInvitation(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) ChangeMemberRole(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) RemoveMember(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) Leave(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) AcceptInvitation(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}

func (s Service) DeclineInvitation(actor uuid.UUID, now time.Time) error {
	panic("not implemented")
}
