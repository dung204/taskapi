package main

import (
	"database/sql"
	"fmt"
	"os"
)

func connectDb(conf config) *sql.DB {
	db, err := sql.Open("pgx", conf.databaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
		defer db.Close()
	}

	return db
}
