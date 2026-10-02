package main

import "os"

type config struct {
	port        string
	databaseURL string
	store       string
}

func getenv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func loadConfig() config {
	return config{
		port:        getenv("PORT", "8080"),
		databaseURL: getenv("DATABASE_URL", ""),
		store:       getenv("STORE", "memory"),
	}
}
