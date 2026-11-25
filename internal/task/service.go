package task

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
	"github.com/dung204/taskapi/internal/metrics"
	"github.com/dung204/taskapi/internal/opaquetoken"
)

type InviteParams struct {
	Email  string
	Role   Role
	Assign bool
}

type Service struct {
	tx                 transactor
	tasks              Store
	invitations        InvitationStore
	members            MemberStore
	users              UserLookup
	outbox             OutboxWriter
	inviteMailLimiter  InviteMailLimiter
	ownerInviteLimiter OwnerInviteLimiter
	metrics            *metrics.Task
}

type ServiceDeps struct {
	Tx                 transactor
	Tasks              Store
	Invitations        InvitationStore
	Members            MemberStore
	Users              UserLookup
	Outbox             OutboxWriter
	InviteMailLimiter  InviteMailLimiter
	OwnerInviteLimiter OwnerInviteLimiter
	Metrics            *metrics.Task
}

const limiterTimeout = 150 * time.Millisecond

func NewService(deps ServiceDeps) *Service {
	return &Service{
		deps.Tx,
		deps.Tasks,
		deps.Invitations,
		deps.Members,
		deps.Users,
		deps.Outbox,
		deps.InviteMailLimiter,
		deps.OwnerInviteLimiter,
		deps.Metrics,
	}
}

func (s Service) Create(ctx context.Context, actor uuid.UUID, p NewParams, now time.Time) (Access, error) {
	p.OwnerID = actor

	t, err := New(p, time.Now())
	if err != nil {
		return Access{}, err
	}

	switch created, err := s.tasks.Create(ctx, t); {
	case err != nil:
		return Access{}, err
	default:
		return Access{
			Task: created,
			Role: RoleOwner,
		}, nil
	}
}

func (s Service) Get(ctx context.Context, actor, id uuid.UUID) (Access, error) {
	return s.tasks.Get(ctx, actor, id)
}

func (s Service) List(ctx context.Context, actor uuid.UUID, f ListFilter) ([]Access, error) {
	return s.tasks.List(ctx, actor, f)
}

func (s Service) Update(ctx context.Context, actor, id uuid.UUID, p Patch, now time.Time) (Access, error) {
	access, err := s.tasks.Get(ctx, actor, id)
	if err != nil {
		return Access{}, err
	}

	required := RequiredForPatch(p)
	switch missing := required &^ rolePerms[access.Role]; {
	case missing&PermEditContent != 0:
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionEditContent)
		return Access{}, ErrCannotEditContent
	case missing&PermChangeStatus != 0:
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionChangeStatus)
		return Access{}, ErrCannotChangeStatus
	}

	err = s.tx.WithTx(ctx, func(st Stores) error {
		t, err := st.Tasks.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}

		if err := t.Patch(p, now); err != nil {
			return err
		}

		if err := st.Tasks.Update(ctx, t); err != nil {
			return err
		}

		a, err := st.Tasks.Get(ctx, actor, id)
		if err != nil {
			return err
		}

		access = a
		return nil
	})
	if err != nil {
		return Access{}, err
	}

	return access, nil
}

func (s Service) Delete(ctx context.Context, actor, id uuid.UUID) error {
	access, err := s.tasks.Get(ctx, actor, id)
	switch {
	case err != nil:
		return err
	case !access.Role.Can(PermDelete):
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionDelete)
		return ErrCannotDelete
	default:
		return s.tasks.Delete(ctx, id)
	}
}

func (s Service) Assign(ctx context.Context, actor, id uuid.UUID, mail string) (Access, error) {
	access, err := s.tasks.Get(ctx, actor, id)
	switch {
	case err != nil:
		return Access{}, err
	case !access.Role.Can(PermAssign):
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionAssign)
		return Access{}, ErrCannotAssign
	}

	mailAddr, err := email.Parse(mail)
	if err != nil {
		return Access{}, err
	}

	userID, err := s.users.GetVerifiedUserIDByEmail(ctx, mailAddr)
	switch {
	case errors.Is(err, ErrUserNotFound):
		return Access{}, ErrAssigneeNotMember
	case err != nil:
		return Access{}, err
	}

	if err = s.tx.WithTx(ctx, func(st Stores) error {
		t, err := st.Tasks.GetForUpdate(ctx, id)
		if err != nil {
			return err
		}

		if userID != t.OwnerID {
			if _, err := st.Members.Get(ctx, id, userID); err != nil {
				if errors.Is(err, ErrUserNotFound) {
					return ErrAssigneeNotMember
				}
				return err
			}
		}

		if err := st.Tasks.SetAssignee(ctx, id, userID); err != nil {
			return err
		}

		a, err := st.Tasks.Get(ctx, actor, id)
		if err != nil {
			return err
		}

		access = a
		return nil
	}); err != nil {
		return Access{}, err
	}

	return access, nil
}

func (s Service) Unassign(ctx context.Context, actor, id uuid.UUID) error {
	access, err := s.tasks.Get(ctx, actor, id)
	switch {
	case err != nil:
		return err
	case !access.Role.Can(PermAssign):
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionAssign)
		return ErrCannotAssign
	default:
		return s.tasks.ClearAssignee(ctx, id)
	}
}

func (s Service) ListShares(ctx context.Context, actor, id uuid.UUID, now time.Time) ([]Share, error) {
	if _, err := s.tasks.Get(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.members.ListShares(ctx, id, now)
}

func (s Service) Invite(ctx context.Context, actor, id uuid.UUID, p InviteParams, now time.Time) (Share, time.Duration, error) {
	switch access, err := s.tasks.Get(ctx, actor, id); {
	case err != nil:
		return Share{}, 0, err
	case !access.Role.Can(PermManageShares):
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionManageShares)
		return Share{}, 0, ErrCannotManageShares
	}

	if !p.Role.IsGrantable() {
		return Share{}, 0, ErrInvalidRole
	}

	mail, err := email.Parse(p.Email)
	if err != nil {
		return Share{}, 0, err
	}

	var inviteeID *uuid.UUID

	switch uid, err := s.users.GetVerifiedUserIDByEmail(ctx, mail); {
	case err == nil:
		inviteeID = &uid
	case errors.Is(err, ErrUserNotFound):
		// do nothing, inviteeID is nil
	default:
		return Share{}, 0, err
	}

	if inviteeID != nil {
		if err := s.precheckMember(ctx, actor, id, *inviteeID); err != nil {
			return Share{}, 0, err
		}
	} else {
		if err := s.precheckPending(ctx, actor, id, mail, now); err != nil {
			return Share{}, 0, err
		}
	}

	rctx, cancel := context.WithTimeout(ctx, limiterTimeout)
	retryAfter, err := s.ownerInviteLimiter.Allow(rctx, actor)
	cancel()
	switch {
	case err != nil:
		return Share{}, 0, err
	case retryAfter > 0:
		return Share{}, retryAfter, nil
	}

	rctx, cancel = context.WithTimeout(ctx, limiterTimeout)
	wait, err := s.inviteMailLimiter.Allow(rctx, mail)
	cancel()
	if err != nil {
		return Share{}, 0, err
	}
	sendMail := wait == 0

	if inviteeID != nil {
		share, err := s.addMember(ctx, actor, id, *inviteeID, mail, p, sendMail, now)
		return share, 0, err
	}

	share, err := s.createPending(ctx, actor, id, mail, p, sendMail, now)
	return share, 0, err
}

func (s Service) precheckMember(ctx context.Context, actor, id, invitee uuid.UUID) error {
	if invitee == actor {
		return ErrCannotInviteSelf
	}

	switch _, err := s.members.Get(ctx, id, invitee); {
	case err == nil:
		return ErrMemberExists
	case errors.Is(err, ErrMemberNotFound):
		// no handling when existing member is not found
	default:
		return err
	}

	// counting members outside transaction for early return if error
	switch cnt, err := s.members.Count(ctx, id); {
	case err != nil:
		return err
	case cnt >= maxMembersPerTask:
		return ErrMembersExceeded
	}

	return nil
}

func (s Service) precheckPending(ctx context.Context, actor, id uuid.UUID, mail string, now time.Time) error {
	switch _, err := s.invitations.GetPending(ctx, id, mail, now); {
	case err == nil:
		return ErrInvitationExists
	case errors.Is(err, ErrInvitationNotFound):
		// no handling when pending invitation is not found
	default:
		return err
	}

	switch cnt, err := s.invitations.CountPendingByOwner(ctx, actor); {
	case err != nil:
		return err
	case cnt >= maxPendingInvitationsPerOwner:
		return ErrPendingInvitationsExceeded
	}

	return nil
}

func (s Service) addMember(ctx context.Context, actor, id, invitee uuid.UUID, mail string, p InviteParams, sendMail bool, now time.Time) (Share, error) {
	inv, err := NewInvitation(NewInvitationParams{
		TaskID:         id,
		Email:          mail,
		Role:           p.Role,
		AssignOnAccept: p.Assign,
	}, now)
	if err != nil {
		return Share{}, err
	}

	// This invitee is a member now so the invitation is accepted
	inv.Accept(now)

	mem, err := NewMember(NewMemberParams{
		TaskID:       id,
		UserID:       invitee,
		Role:         p.Role,
		InvitationID: inv.ID,
	}, now)
	if err != nil {
		return Share{}, err
	}

	var item email.OutboxItem
	if sendMail {
		item, err = email.NewOutboxItem(email.NewOutboxItemParams{
			UserID:  actor,
			Kind:    email.KindTaskAdded,
			ToEmail: mail,
			RefID:   &inv.ID,
		}, now)
		if err != nil {
			return Share{}, err
		}
	}

	err = s.tx.WithTx(ctx, func(st Stores) error {
		if _, err := st.Tasks.GetForUpdate(ctx, id); err != nil {
			return err
		}

		// counting members inside transaction to avoid exceeding max members per task when there's data race
		switch cnt, err := st.Members.Count(ctx, id); {
		case err != nil:
			return err
		case cnt >= maxMembersPerTask:
			return ErrMembersExceeded
		}

		if err := st.Invitations.Create(ctx, inv); err != nil {
			return err
		}

		if err := st.Members.Create(ctx, mem); err != nil {
			return err
		}

		if p.Assign {
			if err := st.Tasks.SetAssignee(ctx, id, invitee); err != nil {
				return err
			}
		}

		if sendMail {
			if err := st.Outbox.Create(ctx, item); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return Share{}, err
	}

	s.metrics.InvitationsInc(metrics.InvitationEventCreated)

	return Share{
		Kind:      ShareKindMember,
		ID:        invitee,
		Email:     mail,
		Role:      p.Role,
		CreatedAt: inv.CreatedAt,
	}, nil
}

func (s Service) createPending(ctx context.Context, actor, id uuid.UUID, mail string, p InviteParams, sendMail bool, now time.Time) (Share, error) {
	inv, err := NewInvitation(NewInvitationParams{
		TaskID:         id,
		Email:          mail,
		Role:           p.Role,
		AssignOnAccept: p.Assign,
	}, now)
	if err != nil {
		return Share{}, err
	}

	var item email.OutboxItem
	if sendMail {
		item, err = email.NewOutboxItem(email.NewOutboxItemParams{
			UserID:  actor,
			Kind:    email.KindTaskInvitation,
			ToEmail: mail,
			RefID:   &inv.ID,
		}, now)
		if err != nil {
			return Share{}, err
		}
	}

	err = s.tx.WithTx(ctx, func(st Stores) error {
		switch cnt, err := st.Invitations.DeleteExpiredPendingFor(ctx, id, mail, now); {
		case err != nil:
			return err
		default:
			s.metrics.ExpiredInvitationsAdd(cnt)
		}

		if err := st.Invitations.Create(ctx, inv); err != nil {
			return err
		}

		if sendMail {
			if err := st.Outbox.Create(ctx, item); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return Share{}, err
	}

	s.metrics.InvitationsInc(metrics.InvitationEventCreated)

	return Share{
		Kind:      ShareKindInvitation,
		ID:        inv.ID,
		Email:     mail,
		Role:      p.Role,
		CreatedAt: inv.CreatedAt,
		ExpiresAt: &inv.ExpiresAt,
	}, nil
}

func (s Service) CancelInvitation(ctx context.Context, actor, id, invitationID uuid.UUID, now time.Time) error {
	switch access, err := s.tasks.Get(ctx, actor, id); {
	case err != nil:
		return err
	case !access.Role.Can(PermManageShares):
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionManageShares)
		return ErrCannotManageShares
	}

	if err := s.invitations.Cancel(ctx, id, invitationID, now); err != nil {
		return err
	}

	s.metrics.InvitationsInc(metrics.InvitationEventCancelled)
	return nil
}

func (s Service) ChangeMemberRole(ctx context.Context, actor, id, userID uuid.UUID, role Role, now time.Time) (Share, error) {
	switch access, err := s.tasks.Get(ctx, actor, id); {
	case err != nil:
		return Share{}, err
	case !access.Role.Can(PermManageShares):
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionManageShares)
		return Share{}, ErrCannotManageShares
	}

	if !role.IsGrantable() {
		return Share{}, ErrInvalidRole
	}

	switch share, err := s.members.SetRole(ctx, id, userID, role, now); {
	case err != nil:
		return Share{}, err
	default:
		return share, nil
	}
}

func (s Service) RemoveMember(ctx context.Context, actor, id, userID uuid.UUID) error {
	switch access, err := s.tasks.Get(ctx, actor, id); {
	case err != nil:
		return err
	case !access.Role.Can(PermManageShares):
		s.metrics.AuthzDeniedInc(metrics.TaskPermissionManageShares)
		return ErrCannotManageShares
	}

	return s.members.Remove(ctx, id, userID)
}

func (s Service) Leave(ctx context.Context, actor, id uuid.UUID) error {
	if _, err := s.tasks.Get(ctx, actor, id); err != nil {
		return err
	}

	return s.members.Remove(ctx, id, actor)
}

func (s Service) AcceptInvitation(ctx context.Context, actor uuid.UUID, token string, now time.Time) (Access, error) {
	tokenHash, err := opaquetoken.Decode(token)
	if err != nil {
		return Access{}, ErrInvalidToken
	}

	var access Access

	err = s.tx.WithTx(ctx, func(st Stores) error {
		inv, err := st.Invitations.GetByTokenHash(ctx, tokenHash)
		switch {
		case errors.Is(err, ErrInvitationNotFound), err == nil && !inv.IsPending(now):
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		switch uid, err := st.Users.GetVerifiedUserIDByEmail(ctx, inv.Email); {
		case errors.Is(err, ErrUserNotFound), err == nil && uid != actor:
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		switch _, err := st.Tasks.GetForUpdate(ctx, inv.TaskID); {
		case errors.Is(err, ErrNotFound):
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		switch cnt, err := st.Members.Count(ctx, inv.TaskID); {
		case err != nil:
			return err
		case cnt >= maxMembersPerTask:
			return ErrMembersExceeded
		}

		switch err := st.Invitations.SetStatus(ctx, inv.TaskID, InvitationStatusAccepted, now); {
		case errors.Is(err, ErrInvitationNotFound):
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		mem, err := NewMember(NewMemberParams{
			TaskID:       inv.TaskID,
			UserID:       actor,
			Role:         inv.Role,
			InvitationID: inv.ID,
		}, now)
		if err != nil {
			return err
		}

		switch err := st.Members.Create(ctx, mem); {
		case errors.Is(err, ErrMemberExists) || errors.Is(err, ErrNotFound):
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		if inv.AssignOnAccept {
			switch err := st.Tasks.SetAssignee(ctx, inv.TaskID, actor); {
			case errors.Is(err, ErrNotFound):
				return ErrInvitationUnavailable
			case err != nil:
				return err
			}
		}

		switch acc, err := st.Tasks.Get(ctx, actor, inv.TaskID); {
		case err == nil:
			access = acc
		case errors.Is(err, ErrNotFound):
			return ErrInvitationUnavailable
		default:
			return err
		}

		return nil
	})
	if err != nil {
		return Access{}, err
	}

	s.metrics.InvitationsInc(metrics.InvitationEventAccepted)

	return access, nil
}

func (s Service) DeclineInvitation(ctx context.Context, actor uuid.UUID, token string, now time.Time) error {
	tokenHash, err := opaquetoken.Decode(token)
	if err != nil {
		return ErrInvalidToken
	}

	err = s.tx.WithTx(ctx, func(st Stores) error {
		inv, err := st.Invitations.GetByTokenHash(ctx, tokenHash)
		switch {
		case errors.Is(err, ErrInvitationNotFound), err == nil && !inv.IsPending(now):
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		switch uid, err := st.Users.GetVerifiedUserIDByEmail(ctx, inv.Email); {
		case errors.Is(err, ErrUserNotFound), err == nil && uid != actor:
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		switch _, err := st.Tasks.GetForUpdate(ctx, inv.TaskID); {
		case errors.Is(err, ErrNotFound):
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		switch err := st.Invitations.SetStatus(ctx, inv.TaskID, InvitationStatusDeclined, now); {
		case errors.Is(err, ErrInvitationNotFound):
			return ErrInvitationUnavailable
		case err != nil:
			return err
		}

		return nil
	})
	if err != nil {
		return err
	}

	s.metrics.InvitationsInc(metrics.InvitationEventDeclined)

	return nil
}
