package oapigen

import (
	"cmp"
	"fmt"
	"time"

	"github.com/dung204/taskapi/internal/apperr"
	"github.com/dung204/taskapi/internal/auth"
	"github.com/dung204/taskapi/internal/task"
)

func ToAPITask(a task.Access) Task {
	t := a.Task

	var assignee *Assignee
	if t.AssigneeID != nil && a.AssigneeEmail != nil {
		assignee = &Assignee{Id: *t.AssigneeID, Email: *a.AssigneeEmail}
	}

	return Task{
		Id:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Status:      t.Status,
		DueAt:       t.DueAt,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		OwnerId:     t.OwnerID,
		Role:        a.Role,
		Assignee:    assignee,
	}
}

func ToAPITasks(as []task.Access) []Task {
	apiTasks := make([]Task, len(as))
	for i, a := range as {
		apiTasks[i] = ToAPITask(a)
	}
	return apiTasks
}

func parseTime(field string, s *string) (*time.Time, error) {
	if s == nil {
		return nil, nil
	}

	t, err := time.Parse(time.RFC3339, *s)
	if err != nil {
		return nil, fmt.Errorf("%w: %s must be an RFC3339 date-time, for example 2030-01-01T00:00:00Z", apperr.ErrInvalidInput, field)
	}

	return &t, nil
}

func (r *CreateTaskRequest) ToTaskNewParams() (task.NewParams, error) {
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
			return task.ListFilter{}, fmt.Errorf("%w: limit is invalid", apperr.ErrInvalidInput)
		}
		limit = min(*p.Limit, maxLimit)
	}

	offset := defaultOffset
	if p.Offset != nil {
		if *p.Offset < 0 {
			return task.ListFilter{}, fmt.Errorf("%w: offset is invalid", apperr.ErrInvalidInput)
		}
		offset = *p.Offset
	}

	var status *task.Status
	if p.Status != nil && *p.Status != "" {
		if !p.Status.IsValidForGet() {
			return task.ListFilter{}, task.ErrInvalidGetStatus
		}
		s := *p.Status
		status = &s
	}

	return task.ListFilter{
		Limit:  limit,
		Offset: offset,
		Status: status,
	}, nil
}

// ParseRefreshToken returns the refresh token of a Logout or RefreshToken
// request and whether it came from the cookie. A token in the body wins; the
// cookie is used when the body has none. When the token comes from the cookie,
// a present Origin header must satisfy originAllowed, otherwise
// apperr.ErrForbidden is returned. A missing token yields an empty string, which
// the service rejects.
func ParseRefreshToken(cookie *RefreshTokenCookie, origin *OriginHeader, body *RefreshRequest, originAllowed func(string) bool) (token string, fromCookie bool, err error) {
	if body != nil && body.RefreshToken != nil && *body.RefreshToken != "" {
		return *body.RefreshToken, false, nil
	}

	if cookie != nil {
		if origin != nil && !originAllowed(*origin) {
			return "", true, apperr.ErrForbidden
		}
		return *cookie, true, nil
	}

	return "", false, nil
}

func (m TokenMode) IsValid() bool {
	return m == Cookie || m == Token
}

func ToAPIUser(u auth.User) User {
	return User{
		Id:            u.ID,
		Email:         u.Email,
		EmailVerified: u.EmailVerifiedAt != nil,
		CreatedAt:     u.CreatedAt,
	}
}

func (r InviteRequest) ToInviteParams() task.InviteParams {
	return task.InviteParams{
		Email:  r.Email,
		Role:   r.Role,
		Assign: *cmp.Or(r.Assign, new(false)),
	}
}

func ToAPITaskShare(s task.Share) Share {
	return Share{
		Id:        s.ID,
		Kind:      ShareKind(s.Kind),
		Email:     s.Email,
		Role:      s.Role,
		CreatedAt: s.CreatedAt,
		ExpiresAt: s.ExpiresAt,
	}
}

func ToAPITaskShares(ss []task.Share) []Share {
	apiShares := make([]Share, len(ss))
	for i, s := range ss {
		apiShares[i] = ToAPITaskShare(s)
	}
	return apiShares
}
