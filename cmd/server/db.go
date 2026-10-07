package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func parseDBURL(dbURL string) (host, dbName string, err error) {
	u, err := url.Parse(dbURL)
	if err != nil {
		return "", "", errors.New("DATABASE_URL is not a valid URL")
	}

	host = u.Hostname()
	dbName = strings.TrimPrefix(u.Path, "/")

	return host, dbName, nil
}

func connectDB(dbURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(10)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = db.PingContext(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}
