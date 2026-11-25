package postgres

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
)

type TaskInvitationStore struct {
	q *sqlcgen.Queries
}

var _ task.InvitationStore = (*TaskInvitationStore)(nil)

func NewInvitationStore(q *sqlcgen.Queries) *TaskInvitationStore {
	return &TaskInvitationStore{q: q}
}

// CountPendingByOwner implements [task.InvitationStore].
func (s *TaskInvitationStore) CountPendingByOwner(ctx context.Context, owner uuid.UUID) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("count", "pending invitations by owner", owner.String(), err)
	}

	count, err := s.q.CountPendingTaskInvitationsByOwner(ctx, owner)
	if err != nil {
		return 0, formatError(err)
	}

	return count, nil
}

// Create implements [task.InvitationStore].
func (s *TaskInvitationStore) Create(ctx context.Context, inv task.Invitation) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "invitation", "", err)
	}

	err := s.q.CreateTaskInvitation(ctx, sqlcgen.CreateTaskInvitationParams(inv))
	pgErr, isPgErr := errors.AsType[*pgconn.PgError](err)

	switch {
	case isPgErr && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "task_invitations_task_id_email_pending_idx":
		return task.ErrInvitationExists
	case isPgErr && pgErr.Code == pgForeignKeyViolation && pgErr.ConstraintName == "task_invitations_task_id_fkey":
		return task.ErrNotFound
	case err != nil:
		return formatError(err)
	default:
		return nil
	}
}

// DeleteClosed implements [task.InvitationStore].
func (s *TaskInvitationStore) DeleteClosed(ctx context.Context, now time.Time, limit int32) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("delete", "closed invitations", "", err)
	}

	deleted, err := s.q.DeleteClosedTaskInvitations(ctx, sqlcgen.DeleteClosedTaskInvitationsParams{
		Now: now, Limit: limit,
	})
	if err != nil {
		return 0, formatError(err)
	}

	return deleted, nil
}

// DeleteExpiredPending implements [task.InvitationStore].
func (s *TaskInvitationStore) DeleteExpiredPending(ctx context.Context, now time.Time, limit int32) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("create", "expired pending invitations", "", err)
	}

	deleted, err := s.q.DeleteExpiredPendingTaskInvitations(ctx, sqlcgen.DeleteExpiredPendingTaskInvitationsParams{
		Now: now, Limit: limit,
	})
	if err != nil {
		return 0, formatError(err)
	}

	return deleted, nil
}

// DeleteExpiredPendingFor implements [task.InvitationStore].
func (s *TaskInvitationStore) DeleteExpiredPendingFor(ctx context.Context, taskID uuid.UUID, email string, now time.Time) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("delete", "expired pending invitation for", email, err)
	}

	cnt, err := s.q.DeleteExpiredPendingTaskInvitationsFor(ctx, sqlcgen.DeleteExpiredPendingTaskInvitationsForParams{
		TaskID: taskID, Email: email, Now: now,
	})
	if err != nil {
		return 0, formatError(err)
	}

	return cnt, nil
}

// GetByTokenHash implements [task.InvitationStore].
func (s *TaskInvitationStore) GetByTokenHash(ctx context.Context, tokenHash []byte) (task.Invitation, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "invitation by token hash", "", err)
	}

	switch row, err := s.q.GetTaskInvitationByTokenHash(ctx, tokenHash); {
	case errors.Is(err, pgx.ErrNoRows):
		return task.Invitation{}, task.ErrInvitationNotFound
	case err != nil:
		return task.Invitation{}, formatError(err)
	default:
		return invitationFromRow(row), nil
	}
}

// GetForMail implements [task.InvitationStore].
func (s *TaskInvitationStore) GetForMail(ctx context.Context, id uuid.UUID) (task.InvitationMail, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "invitation for mail", id.String(), err)
	}

	switch row, err := s.q.GetTaskInvitationForMail(ctx, id); {
	case errors.Is(err, pgx.ErrNoRows):
		return task.InvitationMail{}, task.ErrInvitationNotFound
	case err != nil:
		return task.InvitationMail{}, formatError(err)
	default:
		return invitationMailFromRow(row), nil
	}
}

// SetStatus implements [task.InvitationStore].
func (s *TaskInvitationStore) SetStatus(ctx context.Context, id uuid.UUID, status task.InvitationStatus, now time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("set", "status for task invitation", id.String(), err)
	}

	switch affected, err := s.q.SetTaskInvitationStatus(ctx, sqlcgen.SetTaskInvitationStatusParams{
		ID:        id,
		Status:    status,
		UpdatedAt: now,
	}); {
	case err != nil:
		return formatError(err)
	case affected == 0:
		return task.ErrInvitationNotFound
	default:
		return nil
	}
}

// SetTokenHash implements [task.InvitationStore].
func (s *TaskInvitationStore) SetTokenHash(ctx context.Context, id uuid.UUID, tokenHash []byte, now time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("set", "token hash for task invitation", id.String(), err)
	}

	switch affected, err := s.q.SetTaskInvitationTokenHash(ctx, sqlcgen.SetTaskInvitationTokenHashParams{
		ID: id, TokenHash: tokenHash, UpdatedAt: now,
	}); {
	case err != nil:
		return formatError(err)
	case affected == 0:
		return task.ErrInvitationNotFound
	default:
		return nil
	}
}

// GetPending implements [task.InvitationStore].
func (s *TaskInvitationStore) GetPending(ctx context.Context, taskID uuid.UUID, email string, now time.Time) (task.Invitation, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "pending invitation", "", err)
	}

	switch row, err := s.q.GetPendingTaskInvitation(ctx, sqlcgen.GetPendingTaskInvitationParams{
		TaskID:    taskID,
		Email:     email,
		ExpiresAt: now,
	}); {
	case errors.Is(err, pgx.ErrNoRows):
		return task.Invitation{}, task.ErrInvitationNotFound
	case err != nil:
		return task.Invitation{}, formatError(err)
	default:
		return invitationFromRow(row), nil
	}
}

// Cancel implements [task.InvitationStore].
func (s *TaskInvitationStore) Cancel(ctx context.Context, taskID uuid.UUID, invitationID uuid.UUID, now time.Time) error {
	formatError := func(err error) error {
		return formatPostgresError("cancel", "pending invitation", invitationID.String(), err)
	}

	switch affected, err := s.q.CancelTaskInvitation(ctx, sqlcgen.CancelTaskInvitationParams{
		TaskID:    taskID,
		ID:        invitationID,
		UpdatedAt: now,
	}); {
	case err != nil:
		return formatError(err)
	case affected == 0:
		return task.ErrInvitationNotFound
	default:
		return nil
	}
}

func invitationFromRow(r sqlcgen.TaskInvitation) task.Invitation {
	inv := task.Invitation(r)
	inv.ExpiresAt = utc(inv.ExpiresAt)
	inv.CreatedAt = utc(inv.CreatedAt)
	inv.UpdatedAt = utc(inv.UpdatedAt)

	return inv
}

func invitationMailFromRow(r sqlcgen.GetTaskInvitationForMailRow) task.InvitationMail {
	return task.InvitationMail{
		Invitation: invitationFromRow(r.TaskInvitation),
		TaskTitle:  r.TaskTitle,
		OwnerEmail: r.OwnerEmail,
	}
}
