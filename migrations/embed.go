// Package migrations embeds the goose SQL migrations so Go code (currently the
// Postgres store tests) can apply them without depending on the caller's
// working directory.
package migrations

import "embed"

// FS holds every migration file at its root, as goose expects.
//
//go:embed *.sql
var FS embed.FS
