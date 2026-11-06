// Package moderation implements named reviewer authentication and audited
// moderation decisions. Only the trusted operator CLI uses it; no public HTTP
// route can act on a moderation decision.
package moderation

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// MaxDecisionsPerReviewer forces rotation before a single reviewer can shape an
// unbounded share of enforcement history.
const MaxDecisionsPerReviewer = 100

var (
	ErrInvalidCredentials = errors.New("invalid reviewer credentials")
	ErrRotationRequired   = errors.New("reviewer rotation required")
	ErrResourceNotFound   = errors.New("resource not found")
	ErrAppealNotFound     = errors.New("appeal not found")
	ErrAppealNotOpen      = errors.New("appeal is not open")
	ErrSelfReview         = errors.New("reviewers cannot decide their own appeals")
	ErrInvalidInput       = errors.New("invalid moderation input")
	ErrExists             = errors.New("reviewer already exists")
)

type Reviewer struct {
	ID        string
	Name      string
	Role      string
	Decisions int
}

// Store is satisfied by *pgx.Conn and *pgxpool.Pool.
type Store interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

var contentTables = map[string]string{
	"post":    "social.posts",
	"comment": "social.comments",
	"event":   "social.events",
}

func Authenticate(ctx context.Context, db Store, name, password string) (Reviewer, error) {
	if strings.TrimSpace(name) == "" || password == "" {
		return Reviewer{}, ErrInvalidCredentials
	}
	var r Reviewer
	var hash string
	var active bool
	err := db.QueryRow(ctx, `SELECT reviewer_id,name,role,active,decisions_count,password_hash FROM safety.reviewers WHERE name=$1`, name).
		Scan(&r.ID, &r.Name, &r.Role, &active, &r.Decisions, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reviewer{}, ErrInvalidCredentials
	}
	if err != nil {
		return Reviewer{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || !active {
		return Reviewer{}, ErrInvalidCredentials
	}
	return r, nil
}

// CreateReviewer is an operator-admin action: it needs INSERT on safety.reviewers,
// which the moderator role deliberately lacks.
func CreateReviewer(ctx context.Context, db Store, name, role, password string) (string, error) {
	name = strings.TrimSpace(name)
	if len(name) < 3 || len(name) > 80 || (role != "MODERATOR" && role != "SENIOR") || len(password) < 12 {
		return "", ErrInvalidInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var id string
	err = tx.QueryRow(ctx, `INSERT INTO safety.reviewers(name,role,password_hash) VALUES($1,$2,$3) RETURNING reviewer_id`, name, role, string(hash)).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return "", ErrExists
		}
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO safety.audit(actor,action,resource_id) VALUES($1,'REVIEWER_ADDED',$1)`, id); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return id, nil
}

func SetReviewerActive(ctx context.Context, db Store, reviewer string, active bool) error {
	if !validUUID(reviewer) && strings.TrimSpace(reviewer) == "" {
		return ErrInvalidInput
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	action := "REVIEWER_DISABLED"
	if active {
		action = "REVIEWER_ENABLED"
	}
	var id string
	err = tx.QueryRow(ctx, `UPDATE safety.reviewers SET active=$2 WHERE reviewer_id::text=$1 OR name=$1 RETURNING reviewer_id`, reviewer, active).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrResourceNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO safety.audit(action,resource_id) VALUES($1,$2)`, action, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Decide applies a content status change and attributes it to the authenticated
// reviewer in safety.audit.actor.
func Decide(ctx context.Context, db Store, r Reviewer, kind, resource, decision string) (int64, error) {
	table, ok := contentTables[kind]
	if !ok || (decision != "LIVE" && decision != "REMOVED") || !validUUID(resource) || !validUUID(r.ID) {
		return 0, ErrInvalidInput
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if err = claimDecisionSlot(ctx, tx, r.ID); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, "UPDATE "+table+" SET status=$1 WHERE id=$2", decision, resource)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() != 1 {
		return 0, ErrResourceNotFound
	}
	var id int64
	if err = tx.QueryRow(ctx, `INSERT INTO safety.audit(actor,action,resource_id) VALUES($1,$2,$3) RETURNING id`, r.ID, "MODERATE_"+kind+"_"+decision, resource).Scan(&id); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// ReviewAppeal closes an open appeal. Overturning a REMOVED decision restores
// the content to LIVE; reviewers cannot review an appeal on their own decision.
func ReviewAppeal(ctx context.Context, db Store, r Reviewer, appealID, outcome string) error {
	if (outcome != "UPHELD" && outcome != "OVERTURNED") || !validUUID(appealID) || !validUUID(r.ID) {
		return ErrInvalidInput
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = claimDecisionSlot(ctx, tx, r.ID); err != nil {
		return err
	}
	var status, action string
	var actor, resource *string
	err = tx.QueryRow(ctx, `SELECT a.status,au.actor::text,au.action,au.resource_id::text FROM safety.appeals a JOIN safety.audit au ON au.id=a.decision_id WHERE a.appeal_id=$1 FOR UPDATE OF a`, appealID).
		Scan(&status, &actor, &action, &resource)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrAppealNotFound
	}
	if err != nil {
		return err
	}
	if status != "OPEN" {
		return ErrAppealNotOpen
	}
	if actor != nil && *actor == r.ID {
		return ErrSelfReview
	}
	if _, err = tx.Exec(ctx, `UPDATE safety.appeals SET status=$1,reviewed_by=$2,reviewed_at=now() WHERE appeal_id=$3 AND status='OPEN'`, outcome, r.ID, appealID); err != nil {
		return err
	}
	if outcome == "OVERTURNED" && resource != nil {
		if kind, decision := parseAction(action); kind != "" && decision == "REMOVED" {
			if _, err = tx.Exec(ctx, "UPDATE "+contentTables[kind]+" SET status='LIVE' WHERE id=$1", *resource); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO safety.audit(actor,action,resource_id) VALUES($1,$2,$3)`, r.ID, "APPEAL_"+outcome, appealID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// claimDecisionSlot consumes one rotation slot before the decision is applied.
// The guarded UPDATE keeps the cap exact under concurrency and only needs the
// column privileges the moderator role holds; the caller rolls back on failure.
func claimDecisionSlot(ctx context.Context, tx pgx.Tx, id string) error {
	tag, err := tx.Exec(ctx, `UPDATE safety.reviewers SET decisions_count=decisions_count+1 WHERE reviewer_id=$1 AND active AND decisions_count<$2`, id, MaxDecisionsPerReviewer)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var active bool
	var count int
	err = tx.QueryRow(ctx, `SELECT active,decisions_count FROM safety.reviewers WHERE reviewer_id=$1`, id).Scan(&active, &count)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	return ErrRotationRequired
}

func parseAction(action string) (kind, decision string) {
	rest, ok := strings.CutPrefix(action, "MODERATE_")
	if !ok {
		return "", ""
	}
	for _, k := range []string{"post", "comment", "event"} {
		if d, ok := strings.CutPrefix(rest, k+"_"); ok && (d == "LIVE" || d == "REMOVED") {
			return k, d
		}
	}
	return "", ""
}

func validUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}
