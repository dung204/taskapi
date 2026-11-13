package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dung204/taskapi/internal/store/postgres/sqlcgen"
)

type Transactor[S any] struct {
	pool  *pgxpool.Pool
	build func(q *sqlcgen.Queries) S
}

func NewTransactor[S any](pool *pgxpool.Pool, build func(q *sqlcgen.Queries) S) *Transactor[S] {
	return &Transactor[S]{pool: pool, build: build}
}

func (t *Transactor[S]) WithTx(ctx context.Context, fn func(S) error) error {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(t.build(sqlcgen.New(tx))); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
