package postgres

import (
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dung204/taskapi/internal/apperr"
)

const (
	pgUniqueViolation     = "23505"
	pgForeignKeyViolation = "23503"
)

const (
	pgAdminShutdown    = "57P01"
	pgCrashShutdown    = "57P02"
	pgCannotConnectNow = "57P03"
)

func formatPostgresError(op, itemName, itemValue string, err error) error {
	val := ""
	if itemValue != "" {
		val = " " + itemValue
	}

	if _, ok := errors.AsType[*pgconn.ConnectError](err); ok {
		return fmt.Errorf("postgres: %s %s%s: %w: %w", op, itemName, val, apperr.ErrServiceUnavailable, err)
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		switch pgErr.Code {
		case pgAdminShutdown, pgCrashShutdown, pgCannotConnectNow:
			return fmt.Errorf("postgres: %s %s%s: %w: %w", op, itemName, val, apperr.ErrServiceUnavailable, err)
		}
	}

	return fmt.Errorf("postgres: %s %s%s: %w", op, itemName, val, err)
}
