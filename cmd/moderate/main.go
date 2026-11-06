// Trusted operator-only CLI. No public route can grant verification or publish
// content. Every moderation action is authenticated as a named reviewer and
// audited with that reviewer_id.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"nexus/internal/moderation"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return usage()
	}
	ctx := context.Background()
	switch os.Args[1] {
	case "reviewer":
		return runReviewer(ctx)
	case "appeal":
		return runAppeal(ctx)
	case "post", "comment", "event":
		return runDecision(ctx)
	default:
		return usage()
	}
}

func usage() error {
	return errors.New("usage:\n" +
		"  moderate post|comment|event UUID LIVE|REMOVED   (REVIEWER_NAME, REVIEWER_PASSWORD)\n" +
		"  moderate appeal APPEAL_UUID UPHELD|OVERTURNED   (REVIEWER_NAME, REVIEWER_PASSWORD)\n" +
		"  moderate reviewer add NAME MODERATOR|SENIOR     (ADMIN_DATABASE_URL, REVIEWER_PASSWORD)\n" +
		"  moderate reviewer enable|disable NAME|UUID      (ADMIN_DATABASE_URL)")
}

func connect(ctx context.Context, env string) (*pgx.Conn, error) {
	dsn := os.Getenv(env)
	if dsn == "" {
		return nil, fmt.Errorf("%s required", env)
	}
	c, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func authenticate(ctx context.Context, c moderation.Store) (moderation.Reviewer, error) {
	name, password := os.Getenv("REVIEWER_NAME"), os.Getenv("REVIEWER_PASSWORD")
	if name == "" || password == "" {
		return moderation.Reviewer{}, errors.New("REVIEWER_NAME and REVIEWER_PASSWORD required")
	}
	r, err := moderation.Authenticate(ctx, c, name, password)
	if errors.Is(err, moderation.ErrInvalidCredentials) {
		return moderation.Reviewer{}, errors.New("reviewer authentication failed")
	}
	return r, err
}

func remaining(r moderation.Reviewer) int {
	return moderation.MaxDecisionsPerReviewer - r.Decisions - 1
}

func runDecision(ctx context.Context) error {
	if len(os.Args) != 4 {
		return usage()
	}
	kind, resource, decision := os.Args[1], os.Args[2], os.Args[3]
	if _, err := uuid.Parse(resource); err != nil {
		return err
	}
	if decision != "LIVE" && decision != "REMOVED" {
		return errors.New("invalid decision")
	}
	c, err := connect(ctx, "DATABASE_URL")
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	r, err := authenticate(ctx, c)
	if err != nil {
		return err
	}
	id, err := moderation.Decide(ctx, c, r, kind, resource, decision)
	if err != nil {
		return err
	}
	fmt.Printf("decision %d applied by %s (%d remaining before rotation)\n", id, r.Name, remaining(r))
	return nil
}

func runAppeal(ctx context.Context) error {
	if len(os.Args) != 4 {
		return usage()
	}
	appeal, outcome := os.Args[2], os.Args[3]
	if _, err := uuid.Parse(appeal); err != nil {
		return err
	}
	if outcome != "UPHELD" && outcome != "OVERTURNED" {
		return errors.New("invalid appeal outcome")
	}
	c, err := connect(ctx, "DATABASE_URL")
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	r, err := authenticate(ctx, c)
	if err != nil {
		return err
	}
	if err = moderation.ReviewAppeal(ctx, c, r, appeal, outcome); err != nil {
		return err
	}
	fmt.Printf("appeal %s %s by %s (%d remaining before rotation)\n", appeal, outcome, r.Name, remaining(r))
	return nil
}

func runReviewer(ctx context.Context) error {
	if len(os.Args) < 4 {
		return usage()
	}
	action := os.Args[2]
	c, err := connect(ctx, "ADMIN_DATABASE_URL")
	if err != nil {
		return err
	}
	defer c.Close(ctx)
	switch action {
	case "add":
		if len(os.Args) != 5 {
			return usage()
		}
		name, role := os.Args[3], os.Args[4]
		password := os.Getenv("REVIEWER_PASSWORD")
		if password == "" {
			return errors.New("REVIEWER_PASSWORD required")
		}
		id, err := moderation.CreateReviewer(ctx, c, name, role, password)
		if err != nil {
			return err
		}
		fmt.Printf("reviewer %s (%s) created with id %s\n", name, role, id)
		return nil
	case "enable", "disable":
		if len(os.Args) != 4 {
			return usage()
		}
		if err := moderation.SetReviewerActive(ctx, c, os.Args[3], action == "enable"); err != nil {
			return err
		}
		fmt.Printf("reviewer %s %sd\n", os.Args[3], action)
		return nil
	default:
		return usage()
	}
}
