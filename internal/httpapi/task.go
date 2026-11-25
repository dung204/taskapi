package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"time"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/task"
)

type taskHandler struct {
	service *task.Service
	logger  *slog.Logger
	now     func() time.Time
}

func newTaskHandler(s *task.Service, l *slog.Logger, now func() time.Time) *taskHandler {
	if now == nil {
		now = time.Now
	}

	return &taskHandler{
		service: s,
		logger:  l,
		now:     now,
	}
}

const dbTimeout = 3 * time.Second

func (h *taskHandler) CreateTask(ctx context.Context, request oapigen.CreateTaskRequestObject) (oapigen.CreateTaskResponseObject, error) {
	p, err := request.Body.ToTaskNewParams()
	if err != nil {
		return nil, err
	}

	actor := identityFrom(ctx).UserID
	p.OwnerID = actor

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	acc, err := h.service.Create(dbCtx, actor, p, h.now())
	if err != nil {
		return nil, err
	}

	return oapigen.CreateTask201JSONResponse(oapigen.ToAPITask(acc)), nil
}

func (h *taskHandler) ListTasks(ctx context.Context, request oapigen.ListTasksRequestObject) (oapigen.ListTasksResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	f, err := request.Params.ToListFilter()
	switch {
	case errors.Is(err, apperr.ErrInvalidInput):
		return oapigen.ListTasks400JSONResponse{Error: err.Error()}, nil
	case err != nil:
		return nil, err
	}

	switch accs, err := h.service.List(dbCtx, identityFrom(ctx).UserID, f); {
	case err != nil:
		return nil, err
	default:
		return oapigen.ListTasks200JSONResponse(oapigen.ToAPITasks(accs)), nil
	}
}

func (h *taskHandler) GetTask(ctx context.Context, request oapigen.GetTaskRequestObject) (oapigen.GetTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch acc, err := h.service.Get(dbCtx, identityFrom(ctx).UserID, request.Id); {
	case err != nil:
		return nil, err
	default:
		return oapigen.GetTask200JSONResponse(oapigen.ToAPITask(acc)), nil
	}

}

func (h *taskHandler) UpdateTask(ctx context.Context, request oapigen.UpdateTaskRequestObject) (oapigen.UpdateTaskResponseObject, error) {
	p, err := request.Body.ToPatch()
	switch {
	case errors.Is(err, apperr.ErrInvalidInput):
		return oapigen.UpdateTask400JSONResponse{Error: err.Error()}, nil
	case err != nil:
		return nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch acc, err := h.service.Update(dbCtx, identityFrom(ctx).UserID, request.Id, p, h.now()); {
	case err != nil:
		return nil, err
	default:
		return oapigen.UpdateTask200JSONResponse(oapigen.ToAPITask(acc)), nil
	}
}

func (h *taskHandler) DeleteTask(ctx context.Context, request oapigen.DeleteTaskRequestObject) (oapigen.DeleteTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch err := h.service.Delete(dbCtx, identityFrom(ctx).UserID, request.Id); {
	case err != nil:
		return nil, err
	default:
		return oapigen.DeleteTask204Response{}, nil
	}
}

// ClearAssignee Clear the assignee of a task
// (DELETE /tasks/{id}/assignee)
func (h *taskHandler) ClearAssignee(ctx context.Context, request oapigen.ClearAssigneeRequestObject) (oapigen.ClearAssigneeResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch err := h.service.Unassign(dbCtx, identityFrom(ctx).UserID, request.Id); {
	case err != nil:
		return nil, err
	default:
		return oapigen.ClearAssignee204Response{}, nil
	}
}

// SetAssignee Set the assignee of a task
// (PUT /tasks/{id}/assignee)
func (h *taskHandler) SetAssignee(ctx context.Context, request oapigen.SetAssigneeRequestObject) (oapigen.SetAssigneeResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch acc, err := h.service.Assign(dbCtx, identityFrom(ctx).UserID, request.Id, request.Body.Email); {
	case err != nil:
		return nil, err
	default:
		return oapigen.SetAssignee200JSONResponse(oapigen.ToAPITask(acc)), nil
	}
}

// InviteToTask Share a task with an email address
// (POST /tasks/{id}/invitations)
func (h *taskHandler) InviteToTask(ctx context.Context, request oapigen.InviteToTaskRequestObject) (oapigen.InviteToTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch share, retryAfter, err := h.service.Invite(dbCtx, identityFrom(ctx).UserID, request.Id, request.Body.ToInviteParams(), h.now()); {
	case err != nil:
		return nil, err
	case retryAfter > 0:
		return oapigen.InviteToTask429JSONResponse{
			Headers: oapigen.InviteToTask429ResponseHeaders{
				RetryAfter: max(1, int64(math.Ceil(retryAfter.Seconds()))),
			},
			Body: oapigen.Error{
				Error: apperr.ErrTooManyRequests.Error(),
			},
		}, nil
	default:
		return oapigen.InviteToTask201JSONResponse(oapigen.ToAPITaskShare(share)), nil
	}
}

// CancelInvitation Cancel a pending invitation
// (DELETE /tasks/{id}/invitations/{invitationId})
func (h *taskHandler) CancelInvitation(ctx context.Context, request oapigen.CancelInvitationRequestObject) (oapigen.CancelInvitationResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch err := h.service.CancelInvitation(dbCtx, identityFrom(ctx).UserID, request.Id, request.InvitationId, h.now()); {
	case err != nil:
		return nil, err
	default:
		return oapigen.CancelInvitation204Response{}, nil
	}
}

// LeaveTask Leave a shared task
// (DELETE /tasks/{id}/members/me)
func (h *taskHandler) LeaveTask(ctx context.Context, request oapigen.LeaveTaskRequestObject) (oapigen.LeaveTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch err := h.service.Leave(dbCtx, identityFrom(ctx).UserID, request.Id); {
	case err != nil:
		return nil, err
	default:
		return oapigen.LeaveTask204Response{}, nil
	}
}

// RemoveMember Remove a member
// (DELETE /tasks/{id}/members/{userId})
func (h *taskHandler) RemoveMember(ctx context.Context, request oapigen.RemoveMemberRequestObject) (oapigen.RemoveMemberResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch err := h.service.RemoveMember(dbCtx, identityFrom(ctx).UserID, request.Id, request.UserId); {
	case err != nil:
		return nil, err
	default:
		return oapigen.RemoveMember204Response{}, nil
	}
}

// ChangeMemberRole Change a member's role
// (PATCH /tasks/{id}/members/{userId})
func (h *taskHandler) ChangeMemberRole(ctx context.Context, request oapigen.ChangeMemberRoleRequestObject) (oapigen.ChangeMemberRoleResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch share, err := h.service.ChangeMemberRole(dbCtx, identityFrom(ctx).UserID, request.Id, request.UserId, request.Body.Role, h.now()); {
	case err != nil:
		return nil, err
	default:
		return oapigen.ChangeMemberRole200JSONResponse(oapigen.ToAPITaskShare(share)), nil
	}
}

// ListShares List who a task is shared with
// (GET /tasks/{id}/shares)
func (h *taskHandler) ListShares(ctx context.Context, request oapigen.ListSharesRequestObject) (oapigen.ListSharesResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch shares, err := h.service.ListShares(dbCtx, identityFrom(ctx).UserID, request.Id, h.now()); {
	case err != nil:
		return nil, err
	default:
		return oapigen.ListShares200JSONResponse(oapigen.ToAPITaskShares(shares)), nil
	}
}

// AcceptInvitation Accept an invitation
// (POST /invitations/accept)
func (h *taskHandler) AcceptInvitation(ctx context.Context, request oapigen.AcceptInvitationRequestObject) (oapigen.AcceptInvitationResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch acc, err := h.service.AcceptInvitation(dbCtx, identityFrom(ctx).UserID, request.Body.Token, h.now()); {
	case err != nil:
		return nil, err
	default:
		return oapigen.AcceptInvitation200JSONResponse(oapigen.ToAPITask(acc)), nil
	}
}

// DeclineInvitation Decline an invitation
// (POST /invitations/decline)
func (h *taskHandler) DeclineInvitation(ctx context.Context, request oapigen.DeclineInvitationRequestObject) (oapigen.DeclineInvitationResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	switch err := h.service.DeclineInvitation(dbCtx, identityFrom(ctx).UserID, request.Body.Token, h.now()); {
	case err != nil:
		return nil, err
	default:
		return oapigen.DeclineInvitation204Response{}, nil
	}
}
