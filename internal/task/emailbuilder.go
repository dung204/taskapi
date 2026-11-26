package task

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hako/durafmt"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/opaquetoken"
	"github.com/dung204/taskapi/internal/task/templates"
)

type EmailBuilder struct {
	baseURL     string
	templates   email.Templates
	tasks       Store
	invitations InvitationStore
	now         func() time.Time
}

type EmailBuilderDeps struct {
	BaseURL     string
	Tasks       Store
	Invitations InvitationStore
	Now         func() time.Time
}

func NewEmailBuilder(deps EmailBuilderDeps) (*EmailBuilder, error) {
	tmpls, err := email.LoadTemplates(templates.FS)
	if err != nil {
		return nil, fmt.Errorf("new email builder: %w", err)
	}

	if deps.Now == nil {
		deps.Now = time.Now
	}

	return &EmailBuilder{
		baseURL:     deps.BaseURL,
		templates:   tmpls,
		tasks:       deps.Tasks,
		invitations: deps.Invitations,
		now:         deps.Now,
	}, nil
}

// emailPayload is the data the task_invitation and task_added templates are
// executed with.
type emailPayload struct {
	TaskTitle  string
	OwnerEmail string
	// Role is the role the recipient is invited to or added with.
	Role Role
	// Link is the accept link for an invitation, or the link to the task for a
	// member who was added.
	Link string
	// ExpiresIn is how long the invitation stays valid, for example "7 days".
	// Only the invitation templates use it.
	ExpiresIn string
}

func (b *EmailBuilder) Build(ctx context.Context, item email.ClaimedItem) (email.Message, error) {
	now := b.now()
	if item.RefID == nil {
		return email.Message{}, fmt.Errorf("build email: kind %s requires ref_id", item.Kind)
	}

	inv, err := b.invitations.GetForMail(ctx, *item.RefID)
	switch {
	case errors.Is(err, ErrInvitationNotFound):
		return email.Message{}, email.ErrNothingToSend
	case err != nil:
		return email.Message{}, fmt.Errorf("build email: %w", err)
	case item.Kind == email.KindTaskInvitation && !inv.Invitation.IsPending(now):
		return email.Message{}, email.ErrNothingToSend
	}

	payload := emailPayload{
		TaskTitle:  inv.TaskTitle,
		OwnerEmail: inv.OwnerEmail,
		Role:       inv.Invitation.Role,
	}

	switch item.Kind {
	case email.KindTaskAdded:
		payload.Link = fmt.Sprintf("%s/task/%s", b.baseURL, inv.Invitation.TaskID)
	case email.KindTaskInvitation:
		token, err := opaquetoken.Generate()
		if err != nil {
			return email.Message{}, fmt.Errorf("build email: %w", err)
		}

		payload.Link = fmt.Sprintf("%s/invitations/accept?token=%s", b.baseURL, opaquetoken.Encode(token))
		payload.ExpiresIn = durafmt.Parse(invitationTTL).String()

		err = b.invitations.SetTokenHash(ctx, inv.Invitation.ID, opaquetoken.Hash(token), now)
		switch {
		case errors.Is(err, ErrInvitationNotFound):
			return email.Message{}, email.ErrNothingToSend
		case err != nil:
			return email.Message{}, fmt.Errorf("build email: %w", err)
		}
	default:
		return email.Message{}, fmt.Errorf("build email: unsupported kind %s", item.Kind)
	}

	message, err := b.templates.Render(item.Kind, payload)
	if err != nil {
		return email.Message{}, fmt.Errorf("build email: %w", err)
	}

	message.To = item.ToEmail
	message.Subject = getKindSubject(item.Kind)

	return message, nil
}

func getKindSubject(k email.Kind) string {
	switch k {
	case email.KindTaskInvitation:
		return "You have been invited to a task"
	case email.KindTaskAdded:
		return "You have been added to a task"
	default:
		return ""
	}
}
