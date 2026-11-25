package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
)

type TaskMemberStore struct {
	q *sqlcgen.Queries
}

var _ task.MemberStore = (*TaskMemberStore)(nil)

func NewMemberStore(q *sqlcgen.Queries) *TaskMemberStore {
	return &TaskMemberStore{q: q}
}

// Count implements [task.MemberStore].
func (s *TaskMemberStore) Count(ctx context.Context, taskID uuid.UUID) (int64, error) {
	formatError := func(err error) error {
		return formatPostgresError("count", "task members", taskID.String(), err)
	}

	cnt, err := s.q.CountTaskMembers(ctx, taskID)
	if err != nil {
		return 0, formatError(err)
	}

	return cnt, nil
}

// Create implements [task.MemberStore].
func (s *TaskMemberStore) Create(ctx context.Context, m task.Member) error {
	formatError := func(err error) error {
		return formatPostgresError("create", "task member", "", err)
	}

	err := s.q.CreateTaskMember(ctx, sqlcgen.CreateTaskMemberParams(m))
	pgErr, isPgErr := errors.AsType[*pgconn.PgError](err)

	switch {
	case isPgErr && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == "task_members_pkey":
		return task.ErrMemberExists
	case isPgErr && pgErr.Code == pgForeignKeyViolation && pgErr.ConstraintName == "task_members_task_id_fkey":
		return task.ErrNotFound
	case err != nil:
		return formatError(err)
	default:
		return nil
	}
}

// Get implements [task.MemberStore].
func (s *TaskMemberStore) Get(ctx context.Context, taskID, userID uuid.UUID) (task.Member, error) {
	formatError := func(err error) error {
		return formatPostgresError("get", "task invitation", "", err)
	}

	row, err := s.q.GetTaskMember(ctx, sqlcgen.GetTaskMemberParams{
		TaskID: taskID, UserID: userID,
	})

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return task.Member{}, task.ErrMemberNotFound
	case err != nil:
		return task.Member{}, formatError(err)
	default:
		return memberFromRow(row), nil
	}

}

// ListShares implements [task.MemberStore].
func (s *TaskMemberStore) ListShares(ctx context.Context, taskID uuid.UUID, now time.Time) ([]task.Share, error) {
	formatError := func(err error) error {
		return formatPostgresError("list", "task shares", "", err)
	}

	rows, err := s.q.ListTaskShares(ctx, sqlcgen.ListTaskSharesParams{
		TaskID: taskID, Now: now,
	})
	if err != nil {
		return nil, formatError(err)
	}

	shares := make([]task.Share, len(rows))
	for i := range rows {
		sh, err := shareFromRow(rows[i])
		if err != nil {
			return nil, err
		}
		shares[i] = sh
	}

	return shares, nil
}

// Remove implements [task.MemberStore].
func (s *TaskMemberStore) Remove(ctx context.Context, taskID uuid.UUID, userID uuid.UUID) error {
	formatError := func(err error) error {
		return formatPostgresError("remove", "invitation of task", taskID.String(), err)
	}

	cnt, err := s.q.RemoveTaskMember(ctx, sqlcgen.RemoveTaskMemberParams{
		TaskID: taskID, UserID: userID,
	})

	switch {
	case err != nil:
		return formatError(err)
	case cnt == 0:
		return task.ErrMemberNotFound
	default:
		return nil
	}
}

// SetRole implements [task.MemberStore].
func (s *TaskMemberStore) SetRole(ctx context.Context, taskID, userID uuid.UUID, role task.Role, now time.Time) (task.Share, error) {
	formatError := func(err error) error {
		return formatPostgresError("set", "task member role", "", err)
	}

	row, err := s.q.SetTaskMemberRole(ctx, sqlcgen.SetTaskMemberRoleParams{
		Role: role, Now: now, TaskID: taskID, UserID: userID,
	})

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return task.Share{}, task.ErrMemberNotFound
	case err != nil:
		return task.Share{}, formatError(err)
	default:
		return task.Share{
			Kind:      task.ShareKindMember,
			ID:        row.UserID,
			Email:     row.Email,
			Role:      task.Role(row.Role),
			CreatedAt: utc(row.CreatedAt),
		}, nil
	}
}

func memberFromRow(r sqlcgen.TaskMember) task.Member {
	m := task.Member(r)
	m.CreatedAt = utc(m.CreatedAt)
	m.UpdatedAt = utc(m.UpdatedAt)

	return m
}

func shareFromRow(r sqlcgen.ListTaskSharesRow) (task.Share, error) {
	kind := task.ShareKind(r.Kind)
	switch kind {
	case task.ShareKindMember, task.ShareKindInvitation:
	default:
		return task.Share{}, fmt.Errorf("unknown share kind %q", kind)
	}

	return task.Share{
		Kind:      kind,
		ID:        r.ID,
		Email:     r.Email,
		Role:      r.Role,
		CreatedAt: utc(r.CreatedAt),
		ExpiresAt: utcPtr(r.ExpiresAt),
	}, nil
}
