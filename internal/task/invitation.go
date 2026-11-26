package task

import (
	"slices"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
)

type InvitationStatus string

const (
	InvitationStatusPending   InvitationStatus = "pending"
	InvitationStatusAccepted  InvitationStatus = "accepted"
	InvitationStatusDeclined  InvitationStatus = "declined"
	InvitationStatusCancelled InvitationStatus = "cancelled"
)

var allowedInvitationStatuses = []string{
	string(InvitationStatusPending),
	string(InvitationStatusAccepted),
	string(InvitationStatusDeclined),
	string(InvitationStatusCancelled),
}

func (s InvitationStatus) IsValid() bool {
	return slices.Contains(allowedInvitationStatuses, string(s))
}

type Invitation struct {
	ID             uuid.UUID
	TaskID         uuid.UUID
	Email          string
	Role           Role
	AssignOnAccept bool
	Status         InvitationStatus
	TokenHash      []byte
	ExpiresAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type NewInvitationParams struct {
	TaskID         uuid.UUID
	Email          string
	Role           Role
	AssignOnAccept bool
}

const invitationTTL = 30 * 24 * time.Hour // 30 days

func NewInvitation(p NewInvitationParams, now time.Time) (Invitation, error) {
	id := uuid.NewV7()
	createdAt := now.UTC().Truncate(time.Microsecond)
	expiresAt := now.Add(invitationTTL).UTC().Truncate(time.Microsecond)

	email, err := email.Parse(p.Email)
	if err != nil {
		return Invitation{}, err
	}

	if !p.Role.IsGrantable() {
		return Invitation{}, ErrInvalidRole
	}

	return Invitation{
		ID:             id,
		TaskID:         p.TaskID,
		Email:          email,
		Role:           p.Role,
		AssignOnAccept: p.AssignOnAccept,
		Status:         InvitationStatusPending,
		ExpiresAt:      expiresAt,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}, nil
}

func (inv *Invitation) Accept(now time.Time) {
	inv.Status = InvitationStatusAccepted
}

// IsPending reports whether the invitation can still be answered at now:
// it is pending and has not expired yet.
func (inv Invitation) IsPending(now time.Time) bool {
	return inv.Status == InvitationStatusPending && now.Before(inv.ExpiresAt)
}
