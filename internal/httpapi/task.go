package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/httpapi/oapigen"
	"github.com/dung204/taskapi/internal/task"
)

type taskHandler struct {
	store  task.Store
	logger *slog.Logger
}

func newTaskHandler(s task.Store, l *slog.Logger) *taskHandler {
	return &taskHandler{
		store:  s,
		logger: l,
	}
}

const dbTimeout = 3 * time.Second

func (h *taskHandler) CreateTask(ctx context.Context, request oapigen.CreateTaskRequestObject) (oapigen.CreateTaskResponseObject, error) {
	p, err := request.Body.ToTaskNewParams()
	if err != nil {
		return nil, err
	}

	p.OwnerID = identityFrom(ctx).UserID

	t, err := task.New(p, time.Now())
	if errors.Is(err, apperr.ErrInvalidInput) {
		return oapigen.CreateTask400JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	created, err := h.store.Create(dbCtx, t)
	if err != nil {
		return nil, err
	}

	return oapigen.CreateTask201JSONResponse(oapigen.ToAPITask(created)), nil
}

func (h *taskHandler) ListTasks(ctx context.Context, request oapigen.ListTasksRequestObject) (oapigen.ListTasksResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	f, err := request.Params.ToListFilter()
	if errors.Is(err, apperr.ErrInvalidInput) {
		return oapigen.ListTasks400JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	ts, err := h.store.List(dbCtx, identityFrom(ctx).UserID, f)
	if err != nil {
		return nil, err
	}

	return oapigen.ListTasks200JSONResponse(oapigen.ToAPITasks(ts)), nil
}

func (h *taskHandler) GetTask(ctx context.Context, request oapigen.GetTaskRequestObject) (oapigen.GetTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	t, err := h.store.Get(dbCtx, identityFrom(ctx).UserID, request.Id)
	if errors.Is(err, task.ErrNotFound) {
		return oapigen.GetTask404JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	return oapigen.GetTask200JSONResponse(oapigen.ToAPITask(t)), nil
}

func (h *taskHandler) UpdateTask(ctx context.Context, request oapigen.UpdateTaskRequestObject) (oapigen.UpdateTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	p, err := request.Body.ToPatch()
	if errors.Is(err, apperr.ErrInvalidInput) {
		return oapigen.UpdateTask400JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	t, err := h.store.Update(dbCtx, request.Id, p)
	if errors.Is(err, task.ErrNotFound) {
		return oapigen.UpdateTask404JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	return oapigen.UpdateTask200JSONResponse(oapigen.ToAPITask(t)), nil
}

func (h *taskHandler) DeleteTask(ctx context.Context, request oapigen.DeleteTaskRequestObject) (oapigen.DeleteTaskResponseObject, error) {
	dbCtx, cancel := context.WithTimeout(ctx, dbTimeout)
	defer cancel()

	err := h.store.Delete(dbCtx, request.Id)
	if errors.Is(err, task.ErrNotFound) {
		return oapigen.DeleteTask404JSONResponse{Error: err.Error()}, nil
	}

	if err != nil {
		return nil, err
	}

	return oapigen.DeleteTask204Response{}, nil
}

// ClearAssignee Clear the assignee of a task
// (DELETE /tasks/{id}/assignee)
func (h *taskHandler) ClearAssignee(ctx context.Context, request oapigen.ClearAssigneeRequestObject) (oapigen.ClearAssigneeResponseObject, error) {
	panic("not implemented")
}

// SetAssignee Set the assignee of a task
// (PUT /tasks/{id}/assignee)
func (h *taskHandler) SetAssignee(ctx context.Context, request oapigen.SetAssigneeRequestObject) (oapigen.SetAssigneeResponseObject, error) {
	panic("not implemented")
}

// InviteToTask Share a task with an email address
// (POST /tasks/{id}/invitations)
func (h *taskHandler) InviteToTask(ctx context.Context, request oapigen.InviteToTaskRequestObject) (oapigen.InviteToTaskResponseObject, error) {
	panic("not implemented")
}

// CancelInvitation Cancel a pending invitation
// (DELETE /tasks/{id}/invitations/{invitationId})
func (h *taskHandler) CancelInvitation(ctx context.Context, request oapigen.CancelInvitationRequestObject) (oapigen.CancelInvitationResponseObject, error) {
	panic("not implemented")
}

// LeaveTask Leave a shared task
// (DELETE /tasks/{id}/members/me)
func (h *taskHandler) LeaveTask(ctx context.Context, request oapigen.LeaveTaskRequestObject) (oapigen.LeaveTaskResponseObject, error) {
	panic("not implemented")
}

// RemoveMember Remove a member
// (DELETE /tasks/{id}/members/{userId})
func (h *taskHandler) RemoveMember(ctx context.Context, request oapigen.RemoveMemberRequestObject) (oapigen.RemoveMemberResponseObject, error) {
	panic("not implemented")
}

// ChangeMemberRole Change a member's role
// (PATCH /tasks/{id}/members/{userId})
func (h *taskHandler) ChangeMemberRole(ctx context.Context, request oapigen.ChangeMemberRoleRequestObject) (oapigen.ChangeMemberRoleResponseObject, error) {
	panic("not implemented")
}

// ListShares List who a task is shared with
// (GET /tasks/{id}/shares)
func (h *taskHandler) ListShares(ctx context.Context, request oapigen.ListSharesRequestObject) (oapigen.ListSharesResponseObject, error) {
	panic("not implemented")
}

// AcceptInvitation Accept an invitation
// (POST /invitations/accept)
func (h *taskHandler) AcceptInvitation(ctx context.Context, request oapigen.AcceptInvitationRequestObject) (oapigen.AcceptInvitationResponseObject, error) {
	panic("not implemented")
}

// DeclineInvitation Decline an invitation
// (POST /invitations/decline)
func (h *taskHandler) DeclineInvitation(ctx context.Context, request oapigen.DeclineInvitationRequestObject) (oapigen.DeclineInvitationResponseObject, error) {
	panic("not implemented")
}
