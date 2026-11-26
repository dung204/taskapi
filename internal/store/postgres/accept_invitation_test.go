package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/auth/authtest"
	"github.com/dung204/taskapi/internal/opaquetoken"
	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
	"github.com/dung204/taskapi/internal/task"
	"github.com/dung204/taskapi/internal/testutil"
)

// TestAcceptInvitationNeedsOneConnection is a regression test: with a pool of a
// single connection, AcceptInvitation must still finish. Code that acquires a
// second connection while the transaction holds the first (a store bound to the
// pool called inside WithTx) would wait for itself forever.
func TestAcceptInvitationNeedsOneConnection(t *testing.T) {
	url := testutil.DatabaseURL(t)

	// Seed through a normal pool, then run the service on a one-connection pool.
	seedPool := newAuthPool(t, url)
	q := sqlcgen.New(seedPool)
	users, tasks, invitations := NewUserStore(q), NewTaskStore(q), NewInvitationStore(q)
	ctx := t.Context()

	owner := authtest.SeedUser(ctx, t, users)
	invitee := authtest.SeedUser(ctx, t, users)
	if err := users.SetEmailVerified(ctx, invitee.ID, time.Now()); err != nil {
		t.Fatalf("verify invitee: %v", err)
	}

	now := time.Now()
	nt, err := task.New(task.NewParams{Title: "shared", OwnerID: owner.ID, Status: task.StatusTodo}, now)
	if err != nil {
		t.Fatalf("task.New: %v", err)
	}
	tk, err := tasks.Create(ctx, nt)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	inv, err := task.NewInvitation(task.NewInvitationParams{TaskID: tk.ID, Email: invitee.Email, Role: task.RoleViewer}, now)
	if err != nil {
		t.Fatalf("task.NewInvitation: %v", err)
	}
	// The mail builder stores the hash of the token it sends in the link.
	secret, err := opaquetoken.Generate()
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	inv.TokenHash = opaquetoken.Hash(secret)
	if err := invitations.Create(ctx, inv); err != nil {
		t.Fatalf("create invitation: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cfg.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(one.Close)

	oq := sqlcgen.New(one)
	svc := task.NewService(task.ServiceDeps{
		Tx:          NewTransactor(one, NewTaskStores),
		Tasks:       NewTaskStore(oq),
		Invitations: NewInvitationStore(oq),
		Members:     NewMemberStore(oq),
		Users:       NewUserStore(oq),
		Outbox:      NewOutboxStore(oq),
	})

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	access, err := svc.AcceptInvitation(ctx, invitee.ID, opaquetoken.Encode(secret), now)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("AcceptInvitation did not finish with MaxConns=1: a connection is acquired inside the transaction")
	}
	if err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	if access.Task.ID != tk.ID || access.Role != task.RoleViewer {
		t.Errorf("access = task %v role %q, want task %v role %q", access.Task.ID, access.Role, tk.ID, task.RoleViewer)
	}
}
