// Operator-scheduled DSR fan-out; requires owner-level ADMIN_DATABASE_URL.
// Runs daily after the 30-day grace period. Prints counts only, never identities.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"nexus/internal/retention"
)

func main() {
	ctx := context.Background()
	db, err := pgxpool.New(ctx, os.Getenv("ADMIN_DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "admin database unavailable")
		os.Exit(1)
	}
	defer db.Close()
	n, err := retention.PurgeDue(ctx, db)
	if err != nil {
		fmt.Fprintln(os.Stderr, "DSR purge failed")
		os.Exit(1)
	}
	fmt.Printf("Purged %d account(s)\n", n)
}
