package task

import (
	"time"
	"uuid"
)

type Member struct {
	TaskID       uuid.UUID
	UserID       uuid.UUID
	Role         Role
	InvitationID uuid.UUID
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type NewMemberParams struct {
	TaskID       uuid.UUID
	UserID       uuid.UUID
	Role         Role
	InvitationID uuid.UUID
}

func NewMember(p NewMemberParams, now time.Time) (Member, error) {
	createdAt := now.UTC().Truncate(time.Microsecond)

	if !p.Role.IsGrantable() {
		return Member{}, ErrInvalidRole
	}

	return Member{
		TaskID:       p.TaskID,
		UserID:       p.UserID,
		Role:         p.Role,
		InvitationID: p.InvitationID,
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}, nil
}
