// Trusted operator-only CLI. No public route can grant verification or publish content.
package main

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 4 {
		return fmt.Errorf("usage: moderate post|comment|event UUID LIVE|REMOVED (trusted DB operator only)")
	}
	tables := map[string]string{"post": "social.posts", "comment": "social.comments", "event": "social.events"}
	table, ok := tables[os.Args[1]]
	if !ok {
		return fmt.Errorf("invalid kind")
	}
	if _, err := uuid.Parse(os.Args[2]); err != nil {
		return err
	}
	if os.Args[3] != "LIVE" && os.Args[3] != "REMOVED" {
		return fmt.Errorf("invalid decision")
	}
	ctx := context.Background()
	c, e := pgx.Connect(ctx, os.Getenv("DATABASE_URL"))
	if e != nil {
		return e
	}
	defer c.Close(ctx)
	tx, e := c.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE "+table+" SET status=$1 WHERE id=$2", os.Args[3], os.Args[2])
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("resource not found")
	}
	if _, e = tx.Exec(ctx, "INSERT INTO safety.audit(action,resource_id) VALUES($1,$2)", "MODERATE_"+os.Args[1]+"_"+os.Args[3], os.Args[2]); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	fmt.Println("Moderation decision applied and audited")
	return nil
}
