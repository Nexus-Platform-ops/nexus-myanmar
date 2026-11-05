// Run periodically via your deployment scheduler; no content is logged.
package main

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"os"
)

func main() {
	ctx := context.Background()
	c, err := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "database unavailable")
		os.Exit(1)
	}
	defer c.Close(ctx)
	for _, q := range []string{"DELETE FROM identity.otp WHERE expires_at<now() OR consumed=true", "DELETE FROM identity.sessions WHERE expires_at<now()", "DELETE FROM private.outbox WHERE created_at<now()-interval '24 hours'", "DELETE FROM chat.tombstones WHERE expired_at<now()-interval '30 days'"} {
		if _, err = c.Exec(ctx, q); err != nil {
			fmt.Fprintln(os.Stderr, "retention cleanup failed")
			os.Exit(1)
		}
	}
	fmt.Println("Retention cleanup completed")
}
