package task

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dung204/taskapi/internal/apperr"
)

const (
	maxMembersPerTask             = 50
	maxPendingInvitationsPerOwner = 200
)

// Tasks.
var (
	ErrNotFound           = fmt.Errorf("task: %w", apperr.ErrNotFound)
	ErrInvalidGetStatus   = fmt.Errorf("%w: status must be one of the following: %s", apperr.ErrInvalidInput, strings.Join(allowedGetStatuses, ", "))
	ErrInvalidPatchStatus = fmt.Errorf("%w: status must be one of the following: %s", apperr.ErrInvalidInput, strings.Join(allowedPatchStatuses, ", "))
	ErrCannotEditContent  = fmt.Errorf("%w: your role cannot edit this task", apperr.ErrForbidden)
	ErrCannotChangeStatus = fmt.Errorf("%w: your role cannot change the status of this task", apperr.ErrForbidden)
	ErrCannotAssign       = fmt.Errorf("%w: your role cannot assign this task", apperr.ErrForbidden)
	ErrCannotDelete       = fmt.Errorf("%w: only the owner can delete this task", apperr.ErrForbidden)
	ErrAssigneeNotMember  = fmt.Errorf("%w: the assignee must be the owner or a member of the task", apperr.ErrUnprocessable)
)

// Sharing: members and roles.
var (
	ErrMemberNotFound     = fmt.Errorf("member: %w", apperr.ErrNotFound)
	ErrMemberExists       = fmt.Errorf("%w: this person is already a member of the task", apperr.ErrConflict)
	ErrNotTaskMember      = fmt.Errorf("%w: user is not owner or member of task", apperr.ErrForbidden)
	ErrInvalidRole        = fmt.Errorf("%w: role must be one of the following: %s", apperr.ErrUnprocessable, strings.Join(grantableRoles, ", "))
	ErrCannotManageShares = fmt.Errorf("%w: only the owner can manage who a task is shared with", apperr.ErrForbidden)
	ErrMembersExceeded    = fmt.Errorf("%w: a task can have at most %d members", apperr.ErrUnprocessable, maxMembersPerTask)
	ErrTaskFull           = fmt.Errorf("%w: the task has reached its member limit", apperr.ErrConflict)
)

// Sharing: invitations.
var (
	ErrInvitationNotFound         = fmt.Errorf("invitation: %w", apperr.ErrNotFound)
	ErrInvitationExists           = fmt.Errorf("%w: this email already has a pending invitation for the task", apperr.ErrConflict)
	ErrInvalidInvitationStatus    = fmt.Errorf("%w: status must be one of the following: %s", apperr.ErrInvalidInput, strings.Join(allowedInvitationStatuses, ", "))
	ErrCannotInviteSelf           = fmt.Errorf("%w: you cannot invite yourself", apperr.ErrUnprocessable)
	ErrPendingInvitationsExceeded = fmt.Errorf("%w: you can have at most %d pending invitations", apperr.ErrUnprocessable, maxPendingInvitationsPerOwner)
	ErrInvalidToken               = errors.New("token is invalid")
	ErrInvitationUnavailable      = fmt.Errorf("invitation %w or expired", apperr.ErrNotFound)
)

// Users.
var (
	ErrUserNotFound = errors.New("user not found")
)
