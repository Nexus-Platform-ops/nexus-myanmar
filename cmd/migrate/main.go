package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Migration failed:", err)
		os.Exit(1)
	}
}

var steps = []struct {
	version int
	file    string
}{
	{1, "db/migrations/001_core.sql"},
	{2, "db/migrations/002_privileges.sql"},
	{3, "db/migrations/003_whisper_reads.sql"},
	{4, "db/migrations/004_grants_fix.sql"},
	{5, "db/migrations/005_encrypt_chat.sql"},
}

// operationalLogin roles receive passwords from env after DDL applies; they are
// created NOLOGIN so a missed env fails closed instead of leaving an unlocked role.
var operationalLogin = map[string]string{
	"NEXUS_APP_DB_PASSWORD":       "nexus_app",
	"NEXUS_MODERATOR_DB_PASSWORD": "nexus_moderator",
	"NEXUS_JANITOR_DB_PASSWORD":   "nexus_janitor",
}

func run() error {
	ctx := context.Background()
	c, e := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	defer c.Close(ctx)
	if _, e = c.Exec(ctx, "SELECT pg_advisory_lock(9942101)"); e != nil {
		return e
	}
	defer c.Exec(ctx, "SELECT pg_advisory_unlock(9942101)")
	var v int
	e = c.QueryRow(ctx, "SELECT version FROM identity.schema_version").Scan(&v)
	if e != nil {
		var pgErr *pgconn.PgError
		if errors.Is(e, pgx.ErrNoRows) || (errors.As(e, &pgErr) && pgErr.Code == "42P01") {
			v = 0
		} else {
			return e
		}
	}
	for _, s := range steps {
		if v >= s.version {
			continue
		}
		b, e := os.ReadFile(s.file)
		if e != nil {
			return e
		}
		if s.version == 5 {
			// 005 encrypts existing bodies in place; it must run under the same
			// key the API reads with, and must fail closed without one.
			key := os.Getenv("DATA_KEY")
			if len(key) < 32 {
				return fmt.Errorf("DATA_KEY (>=32 bytes) required for schema version 5 (encrypting chat bodies)")
			}
			if _, e = c.Exec(ctx, "SELECT set_config('nexus.data_key',$1,false)", key); e != nil {
				return e
			}
		}
		if _, e = c.Exec(ctx, string(b)); e != nil {
			return fmt.Errorf("step %d: %w", s.version, e)
		}
		v = s.version
		fmt.Printf("Applied schema version %d\n", s.version)
	}
	fmt.Printf("Schema at version %d\n", v)
	if v < 2 {
		return nil
	}
	return assignRolePasswords(ctx, c)
}

func assignRolePasswords(ctx context.Context, c *pgx.Conn) error {
	seen := map[string]bool{}
	for env, role := range operationalLogin {
		pw := os.Getenv(env)
		if pw == "" {
			return fmt.Errorf("%s required after schema version 2 (role %s is NOLOGIN until set)", env, role)
		}
		if len(pw) < 16 {
			return fmt.Errorf("%s must be at least 16 characters", env)
		}
		if seen[pw] {
			return fmt.Errorf("operational role passwords must be distinct (%s reuses another)", env)
		}
		seen[pw] = true
		// ALTER ROLE is a utility statement and rejects bind parameters; the
		// literal must be single-quote escaped inline.
		literal := strings.ReplaceAll(pw, "'", "''")
		if _, e := c.Exec(ctx, "ALTER ROLE "+role+" WITH LOGIN PASSWORD '"+literal+"'"); e != nil {
			return fmt.Errorf("activating role %s: %w", role, e)
		}
	}
	return nil
}
