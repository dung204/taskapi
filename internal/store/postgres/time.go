package postgres

import "time"

// utc converts t to UTC. pgx returns timestamptz values in time.Local, so every
// time read from the database goes through this (or [utcPtr]) in the mappers.
func utc(t time.Time) time.Time {
	return t.UTC()
}

// utcPtr is [utc] for nullable columns: it returns nil for nil, and otherwise a
// pointer to a new UTC value, leaving *t untouched.
func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}

	return new(t.UTC())
}
