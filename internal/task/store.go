package task

import (
	"context"
	"time"
	"uuid"

	"github.com/dung204/taskapi/internal/email"
)

type Scope string

const (
	ScopeAll    Scope = "all"
	ScopeOwned  Scope = "owned"
	ScopeShared Scope = "shared"
)

type ListFilter struct {
	Limit        int
	Offset       int
	Status       *Status // nil means no filter
	Scope        Scope
	AssignedToMe bool
}

type Patch struct {
	Title       *string
	Description *string
	Status      *Status
	DueAt       *time.Time
}

type Access struct {
	Task          Task
	Role          Role
	AssigneeEmail *string // nil when task is not assigned to anyone
}

// Store persists tasks. Every implementation must behave the same.
type Store interface {
	// Create persists a [Task]. It does not validate the task: build it with
	// [New] first.
	//
	// Returns:
	//   - task - the [Task] that was persisted.
	Create(ctx context.Context, t Task) (task Task, err error)

	// Get fetches the [Task] with the given id as seen by viewer.
	//
	// Returns:
	//   - access - an [Access] holding the [Task] with the given id, viewer's
	//     [Role] on it, and the email of its assignee (nil if it has none).
	//
	// Returns the following errors:
	//   - [ErrNotFound] - if the task does not exist or viewer is not allowed to
	//     see it (the two cases are indistinguishable on purpose).
	Get(ctx context.Context, viewer, id uuid.UUID) (access Access, err error)

	// List fetches the [Task]s visible to viewer, newest first (by id,
	// descending). A nil f.Status means no status filter; otherwise only the
	// tasks with that status are kept. f.Limit and f.Offset page the result.
	//
	// Returns:
	//   - accesses - the matching [Access]es (each holding a [Task] and the
	//     viewer's [Role] on it), or an empty, non-nil slice if none match.
	List(ctx context.Context, viewer uuid.UUID, f ListFilter) (accesses []Access, err error)

	// GetForUpdate fetches the [Task] with the given id for a read-modify-write
	// cycle. Unlike [Store.Get] it does not look at who is asking, so the caller
	// must check access itself. Inside a transaction (see [transactor.WithTx]) it
	// keeps concurrent changes to the task from interleaving with the caller's
	// until the transaction ends.
	//
	// Returns:
	//   - task - the [Task] with the given id.
	//
	// Returns the following errors:
	//   - [ErrNotFound] - if the task does not exist.
	GetForUpdate(ctx context.Context, id uuid.UUID) (task Task, err error)

	// Update persists the title, description, status, due date and update time of
	// the task with the ID of t. It does not validate t and leaves the other
	// fields (such as the owner and the assignee) alone: apply a [Patch] with
	// [Task.Patch] first, usually to a task fetched with [Store.GetForUpdate].
	//
	// Returns the following errors:
	//   - [ErrNotFound] - if the task does not exist.
	Update(ctx context.Context, t Task) error

	// Delete removes the task with the given id, together with its members and
	// invitations. It does not look at who is asking, so the caller must check
	// access itself.
	//
	// Returns the following errors:
	//   - [ErrNotFound] - if the task does not exist.
	Delete(ctx context.Context, id uuid.UUID) error

	// MarkOverdue sets the status of every task, whoever owns it, whose due date
	// is before now to overdue, and records now as its update time. Tasks that
	// are already overdue or done, and tasks without a due date, are left alone.
	// Running it again with the same now changes nothing.
	//
	// Returns:
	//   - marked - the number of tasks whose status was changed to overdue.
	MarkOverdue(ctx context.Context, now time.Time) (marked int64, err error)

	// SetAssignee assigns the task with the given id to the user assigneeID,
	// replacing any previous assignee. It does not check that the user may be
	// assigned (for example that they are a member of the task).
	//
	// Returns the following errors:
	//   - [ErrNotFound] - if the task does not exist.
	SetAssignee(ctx context.Context, id, assigneeID uuid.UUID) error

	// ClearAssignee removes the assignee of the task with the given id, whoever
	// it is. Clearing a task that has no assignee is not an error.
	//
	// Returns the following errors:
	//   - [ErrNotFound] - if the task does not exist.
	ClearAssignee(ctx context.Context, id uuid.UUID) error

	// ClearAssigneeIf removes the assignee of the task with the given id only if
	// the assignee is the user userID, for example when that user leaves the
	// task. It does nothing if the task has another assignee or none, or if the
	// task does not exist.
	ClearAssigneeIf(ctx context.Context, id, userID uuid.UUID) error
}

type InvitationMail struct {
	Invitation Invitation
	TaskTitle  string
	OwnerEmail string
}

// InvitationStore persists invitations to collaborate on a task.
type InvitationStore interface {
	// Create persists an [Invitation]. It does not validate the invitation:
	// build it with [NewInvitation] first.
	//
	// Returns the following errors:
	//   - [ErrInvitationExists] - if the invitee's email already has a pending
	//     invitation to the task (one that has expired still counts until it is
	//     removed with [InvitationStore.DeleteExpiredPendingFor]).
	//   - [ErrNotFound] - if the task does not exist.
	Create(ctx context.Context, inv Invitation) error

	// GetByTokenHash fetches the [Invitation] whose token hash equals tokenHash.
	//
	// Returns:
	//   - invitation - the matching [Invitation].
	//
	// Returns the following errors:
	//   - [ErrInvitationNotFound] - if no invitation matches.
	GetByTokenHash(ctx context.Context, tokenHash []byte) (invitation Invitation, err error)

	// GetForMail fetches the [Invitation] with the given id together with what
	// the invitation email needs.
	//
	// Returns:
	//   - mail - an [InvitationMail] holding the [Invitation], the title of its
	//     task and the email of the task's owner.
	//
	// Returns the following errors:
	//   - [ErrInvitationNotFound] - if no invitation has the given id.
	GetForMail(ctx context.Context, id uuid.UUID) (mail InvitationMail, err error)

	// SetStatus sets the status of the invitation with the given id and records
	// now as its update time. It does not check that the transition is allowed.
	//
	// Returns the following errors:
	//   - [ErrInvitationNotFound] - if the invitation does not exist.
	SetStatus(ctx context.Context, id uuid.UUID, status InvitationStatus, now time.Time) error

	// SetTokenHash replaces the token hash of the invitation with the given id
	// (so the old link stops working) and records now as its update time.
	//
	// Returns the following errors:
	//   - [ErrInvitationNotFound] - if the invitation does not exist.
	SetTokenHash(ctx context.Context, id uuid.UUID, tokenHash []byte, now time.Time) error

	// DeleteExpiredPendingFor removes the pending invitation of email to the
	// task taskID if it expired at or before now, so a new one can be created in
	// its place. Deleting nothing is not an error.
	DeleteExpiredPendingFor(ctx context.Context, taskID uuid.UUID, email string, now time.Time) (int64, error)

	// CountPendingByOwner counts the pending invitations on tasks owned by
	// owner, used to cap how many invitations an owner can have open. It does
	// not look at expiry.
	//
	// Returns:
	//   - count - the number of invitations.
	CountPendingByOwner(ctx context.Context, owner uuid.UUID) (count int64, err error)

	// DeleteExpiredPending removes up to limit pending invitations that expire at
	// or before now.
	//
	// Returns:
	//   - deleted - the number of invitations removed.
	DeleteExpiredPending(ctx context.Context, now time.Time, limit int32) (deleted int64, err error)

	// DeleteClosed removes up to limit declined or cancelled invitations that
	// expire at or before now.
	//
	// Returns:
	//   - deleted - the number of invitations removed.
	DeleteClosed(ctx context.Context, now time.Time, limit int32) (deleted int64, err error)

	// GetPending returns the unexpired pending invitation for the given task and
	// (already normalized) email.
	//
	// Returns the following errors:
	//   - [ErrInvitationNotFound] - if there is none, including one that is
	//     closed (accepted, declined, cancelled) or already expired.
	GetPending(ctx context.Context, taskID uuid.UUID, email string, now time.Time) (Invitation, error)

	// Cancel marks the pending, unexpired invitation as cancelled.
	//
	// Returns the following errors:
	//   - [ErrInvitationNotFound] - if no unexpired pending invitation with the
	//     given id belongs to the task, including one already closed or expired.
	Cancel(ctx context.Context, taskID, invitationID uuid.UUID, now time.Time) error
}

type ShareKind string

const (
	ShareKindMember     ShareKind = "member"
	ShareKindInvitation ShareKind = "invitation"
)

type Share struct {
	Kind      ShareKind
	ID        uuid.UUID
	Email     string
	Role      Role
	CreatedAt time.Time
	ExpiresAt *time.Time // nil when Kind = "member"
}

// MemberStore persists the members of a task: the users an owner has shared
// the task with, each holding a [Role].
type MemberStore interface {
	// Create persists a [Member]. It does not validate the member: build it with
	// [NewMember] first.
	//
	// Returns the following errors:
	//   - [ErrMemberExists] - if the user is already a member of the task.
	//   - [ErrNotFound] - if the task does not exist.
	Create(ctx context.Context, m Member) error

	// Get fetches the [Member] of the task taskID that is the user userID.
	//
	// Returns:
	//   - member - the matching [Member].
	//
	// Returns the following errors:
	//   - [ErrMemberNotFound] - if the user is not a member of the task.
	Get(ctx context.Context, taskID, userID uuid.UUID) (member Member, err error)

	// SetRole sets the role of a member of the task and records now as the
	// member's update time. It does not check that role can be granted.
	//
	// Returns:
	//   - share - the [Share] of the updated member (its Kind is
	//     [ShareKindMember] and its ExpiresAt is nil).
	//
	// Returns the following errors:
	//   - [ErrMemberNotFound] - if the user is not a member of the task.
	SetRole(ctx context.Context, taskID, userID uuid.UUID, role Role, now time.Time) (share Share, err error)

	// Remove deletes a member from the task.
	//
	// Returns the following errors:
	//   - [ErrMemberNotFound] - if the user is not a member of the task.
	Remove(ctx context.Context, taskID, userID uuid.UUID) error

	// Count counts the members of the task.
	//
	// Returns:
	//   - count - the number of members (pending invitations are not counted).
	Count(ctx context.Context, taskID uuid.UUID) (count int64, err error)

	// ListShares fetches who the task is shared with: its members and its
	// pending invitations that have not expired at now, in no particular order.
	//
	// Returns:
	//   - shares - the [Share]s of the task, or an empty, non-nil slice if it has
	//     none (including when the task does not exist).
	ListShares(ctx context.Context, taskID uuid.UUID, now time.Time) (shares []Share, err error)
}

type UserLookup interface {
	// GetUserIDByEmail returns the ID of the verified, enabled account with the
	// given (already normalized) email address.
	//
	// Returns the following errors:
	//   - [ErrUserNotFound] - if there is no such account, including an account
	//     whose email is not verified yet or that is disabled.
	GetVerifiedUserIDByEmail(ctx context.Context, email string) (userID uuid.UUID, err error)
}

type OutboxWriter interface {
	// Create persists an [email.OutboxItem] to be delivered later.
	Create(ctx context.Context, item email.OutboxItem) error
}

type Stores struct {
	Tasks       Store
	Invitations InvitationStore
	Members     MemberStore
	Outbox      OutboxWriter
	Users       UserLookup
}

// transactor runs several store operations as one unit.
type transactor interface {
	// WithTx runs fn with [Stores] that all work inside a single transaction. The
	// transaction is committed if fn returns nil and rolled back if fn returns an
	// error or the commit fails.
	//
	// Returns the following errors:
	//   - the error of fn - unchanged, if fn fails.
	//   - an error - if the transaction could not be started or committed.
	WithTx(ctx context.Context, fn func(st Stores) error) error
}
