package testutil

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// templateEnv names the variable that holds the URL of the migrated template
// database. Tests never run on it: [RunMain] clones it for each test package.
const templateEnv = "TEST_DATABASE_URL"

// cloneLockKey is the advisory lock that serialises CREATE DATABASE ... TEMPLATE
// between test binaries running in parallel.
const cloneLockKey = 7_301_204_117

var (
	// cloneURL is the URL of this package's private database, "" when there is none.
	cloneURL string
	// isCI is true when the CI variable is set: a missing database is then an error.
	isCI = os.Getenv("CI") != ""
)

// RunMain is for TestMain of every package that needs Postgres:
//
//	func TestMain(m *testing.M) { os.Exit(testutil.RunMain(m)) }
//
// It clones the template database named by TEST_DATABASE_URL (already migrated,
// see scripts/test.sh) into a database private to this test binary, runs the
// tests and drops the clone. Packages run in parallel under "go test ./...", so
// two of them must never truncate the same database.
//
// Without TEST_DATABASE_URL the tests that call [DatabaseURL] skip locally; with
// CI set RunMain fails instead, so a CI run cannot go green without them. It
// refuses to touch a database whose name does not contain "test".
func RunMain(m *testing.M) int {
	raw := strings.TrimSpace(os.Getenv(templateEnv))
	if raw == "" {
		if isCI {
			fmt.Fprintf(os.Stderr, "%s is not set but CI is: Postgres tests must not be skipped\n", templateEnv)
			return 1
		}
		return m.Run()
	}

	ctx := context.Background()
	u, name, err := cloneDatabase(ctx, raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres tests: %v\n", err)
		return 1
	}
	cloneURL = u
	code := m.Run()
	if err := dropDatabase(ctx, raw, name); err != nil {
		fmt.Fprintf(os.Stderr, "postgres tests: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

// DatabaseURL returns the URL of the database private to this test package.
// Tests may create and delete rows freely but must not assume the tables are
// empty, because other tests of the package share them. Without a database it
// skips the test, or fails it when CI is set.
func DatabaseURL(tb testing.TB) string {
	tb.Helper()
	if cloneURL == "" {
		if isCI {
			tb.Fatalf("%s is not set but CI is: Postgres tests must not be skipped", templateEnv)
		}
		tb.Skipf("%s not set", templateEnv)
	}
	return cloneURL
}

// requireTestName is the safety guard: tests truncate tables.
func requireTestName(name string) error {
	if !strings.Contains(name, "test") {
		return fmt.Errorf("refusing to use database %q: its name must contain \"test\"", name)
	}
	return nil
}

var unsafeChars = regexp.MustCompile(`[^a-z0-9_]+`)

// adminURL returns raw with the database replaced, and the template's name.
func withDatabase(raw, db string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", templateEnv, err)
	}
	u.Path = "/" + db
	return u.String(), nil
}

func templateName(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", templateEnv, err)
	}
	name := strings.TrimPrefix(u.Path, "/")
	if name == "" {
		return "", fmt.Errorf("%s has no database name", templateEnv)
	}
	return name, requireTestName(name)
}

func cloneDatabase(ctx context.Context, raw string) (cloneURL, name string, err error) {
	tmpl, err := templateName(raw)
	if err != nil {
		return "", "", err
	}
	pkg := unsafeChars.ReplaceAllString(strings.ToLower(filepath.Base(os.Args[0])), "_")
	name = fmt.Sprintf("%s_%s_%d", tmpl, pkg, os.Getpid())
	if len(name) > 63 { // the identifier limit of Postgres
		name = name[len(name)-63:]
	}
	if err := requireTestName(name); err != nil {
		return "", "", err
	}

	admin, err := withDatabase(raw, "postgres")
	if err != nil {
		return "", "", err
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return "", "", fmt.Errorf("connect to the maintenance database: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", cloneLockKey); err != nil {
		return "", "", fmt.Errorf("lock: %w", err)
	}
	defer func() { _, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", cloneLockKey) }()

	// A leftover from a crashed run with the same pid is replaced.
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
		return "", "", fmt.Errorf("drop stale clone: %w", err)
	}

	// Postgres refuses to clone a template that has connections (55006); the
	// ones that exist are short-lived, so retry for a while.
	stmt := "CREATE DATABASE " + pgx.Identifier{name}.Sanitize() + " TEMPLATE " + pgx.Identifier{tmpl}.Sanitize()
	deadline := time.Now().Add(30 * time.Second)
	for {
		_, err = conn.Exec(ctx, stmt)
		var pgErr *pgconn.PgError
		if err == nil || !errors.As(err, &pgErr) || pgErr.Code != "55006" || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return "", "", fmt.Errorf("clone template %q: %w (is it migrated and free of connections?)", tmpl, err)
	}

	cloneURL, err = withDatabase(raw, name)
	return cloneURL, name, err
}

func dropDatabase(ctx context.Context, raw, name string) error {
	if err := requireTestName(name); err != nil {
		return err
	}
	admin, err := withDatabase(raw, "postgres")
	if err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return fmt.Errorf("connect to drop %q: %w", name, err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
		return fmt.Errorf("drop %q: %w", name, err)
	}
	return nil
}
