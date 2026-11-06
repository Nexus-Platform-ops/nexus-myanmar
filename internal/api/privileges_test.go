package api

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestPrivilegeBoundaries proves the physical grant shape from db/migrations/002_privileges.sql:
// each operational role reaches exactly its tables and nothing else. This is the
// regression guard for the P0-1 dual-profile isolation gap — a future migration
// that widens grants fails here.

func roleDSN(t *testing.T, env, user, fallbackPW string) string {
	t.Helper()
	if v := os.Getenv(env); v != "" {
		return v
	}
	if os.Getenv("NEXUS_INTEGRATION") != "1" {
		t.Skip("requires migrated nexus_test database")
	}
	return "postgres://" + user + ":" + fallbackPW + "@localhost:5432/nexus_test?sslmode=disable"
}

func connectRole(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("role cannot connect: %v", err)
	}
	t.Cleanup(func() { c.Close(context.Background()) })
	return c
}

func denied(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s unexpectedly allowed", what)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("%s failed with non-Postgres error: %v", what, err)
	}
	if pgErr.Code != "42501" {
		t.Fatalf("%s: want permission denied (42501), got %s: %s", what, pgErr.Code, pgErr.Message)
	}
}

func TestPrivilegeBoundaries(t *testing.T) {
	if os.Getenv("NEXUS_INTEGRATION") != "1" {
		t.Skip("requires migrated nexus_test database and Redis")
	}
	ctx := context.Background()

	t.Run("app_has_no_ddl", func(t *testing.T) {
		c := connectRole(t, roleDSN(t, "TEST_APP_DATABASE_URL", "nexus_app", "nexus_app_ci_only_pw"))
		var v int
		if err := c.QueryRow(ctx, "SELECT version FROM identity.schema_version").Scan(&v); err != nil || v < 2 {
			t.Fatalf("app role cannot read schema_version: v=%d err=%v", v, err)
		}
		if _, err := c.Exec(ctx, "CREATE TABLE social.privilege_probe(id int)"); err == nil {
			t.Fatal("app role created a table")
		}
		if _, err := c.Exec(ctx, "ALTER TABLE social.posts ADD COLUMN probe int"); err == nil {
			t.Fatal("app role altered a table")
		}
		denied(t, execErr(c, ctx, "TRUNCATE chat.messages"), "app TRUNCATE")
		denied(t, execErr(c, ctx, "DROP TABLE social.posts"), "app DROP")
		denied(t, execErr(c, ctx, "CREATE ROLE privilege_probe LOGIN"), "app CREATE ROLE")
		denied(t, execErr(c, ctx, "SELECT name FROM safety.reviewers LIMIT 1"), "app reviewer read")
	})

	t.Run("moderator_scoped", func(t *testing.T) {
		c := connectRole(t, roleDSN(t, "TEST_MODERATOR_DATABASE_URL", "nexus_moderator", "nexus_mod_ci_only_pw1"))
		if _, err := c.Exec(ctx, "INSERT INTO safety.audit(action,resource_id) VALUES('PROBE',gen_random_uuid())"); err != nil {
			t.Fatalf("moderator audit insert: %v", err)
		}
		var reviewers int
		if err := c.QueryRow(ctx, "SELECT count(*) FROM safety.reviewers").Scan(&reviewers); err != nil {
			t.Fatalf("moderator reviewer read: %v", err)
		}
		if _, err := c.Exec(ctx, "UPDATE safety.reviewers SET decisions_count=decisions_count WHERE false"); err != nil {
			t.Fatalf("moderator rotation counter update: %v", err)
		}
		denied(t, execErr(c, ctx, "UPDATE safety.reviewers SET password_hash='x' WHERE false"), "moderator reviewer password write")
		denied(t, execErr(c, ctx, "INSERT INTO safety.reviewers(name,role,password_hash) VALUES('probe','MODERATOR','x')"), "moderator reviewer create")
		denied(t, execErr(c, ctx, "SELECT id FROM dating.dating_profiles LIMIT 1"), "moderator dating read")
		denied(t, execErr(c, ctx, "SELECT user_id FROM private.user_locations LIMIT 1"), "moderator location read")
		denied(t, execErr(c, ctx, "SELECT id FROM identity.accounts LIMIT 1"), "moderator identity read")
		denied(t, execErr(c, ctx, "SELECT body_cipher FROM chat.messages LIMIT 1"), "moderator message read")
		denied(t, execErr(c, ctx, "DELETE FROM social.posts WHERE false"), "moderator post delete")
		denied(t, execErr(c, ctx, "INSERT INTO social.posts(author,body,township,audience) VALUES(gen_random_uuid(),'x','k','PUBLIC')"), "moderator post write")
	})

	t.Run("analytics_aggregate_only", func(t *testing.T) {
		c := connectRole(t, roleDSN(t, "TEST_ANALYTICS_DATABASE_URL", "nexus_analytics", "nexus_analytics_ci_only_pw3"))
		for _, q := range []string{
			"SELECT township,day,posts FROM analytics.post_volume",
			"SELECT township,day,events FROM analytics.event_volume",
			"SELECT day,mode,messages FROM analytics.message_volume",
			"SELECT day,category,reports FROM analytics.report_volume",
			"SELECT day,sessions FROM analytics.session_volume",
		} {
			if _, err := c.Exec(ctx, q); err != nil {
				t.Fatalf("analytics view read %q: %v", q, err)
			}
		}
		denied(t, execErr(c, ctx, "SELECT body FROM social.posts LIMIT 1"), "analytics post body read")
		denied(t, execErr(c, ctx, "SELECT phone_hash FROM identity.accounts LIMIT 1"), "analytics identity read")
		denied(t, execErr(c, ctx, "SELECT body_cipher FROM chat.messages LIMIT 1"), "analytics message read")
		denied(t, execErr(c, ctx, "SELECT target_id FROM safety.reports LIMIT 1"), "analytics report target read")
		denied(t, execErr(c, ctx, "SELECT nickname FROM dating.dating_profiles LIMIT 1"), "analytics dating read")
		denied(t, execErr(c, ctx, "CREATE TABLE analytics.probe(id int)"), "analytics DDL")
	})

	t.Run("janitor_scoped", func(t *testing.T) {
		c := connectRole(t, roleDSN(t, "TEST_JANITOR_DATABASE_URL", "nexus_janitor", "nexus_jan_ci_only_pw2"))
		if _, err := c.Exec(ctx, "DELETE FROM chat.tombstones WHERE expired_at<now() AND false"); err != nil {
			t.Fatalf("janitor tombstone delete: %v", err)
		}
		if _, err := c.Exec(ctx, `WITH expired AS (DELETE FROM chat.messages WHERE mode='WHISPER' AND whisper_expires_at<=now() RETURNING id,room_id,seq) INSERT INTO chat.tombstones(message_id,room_id,seq) SELECT id,room_id,seq FROM expired ON CONFLICT DO NOTHING`); err != nil {
			t.Fatalf("janitor whisper sweep: %v", err)
		}
		if _, err := c.Exec(ctx, "DELETE FROM identity.idempotency WHERE created_at<now() AND false"); err != nil {
			t.Fatalf("janitor idempotency cleanup: %v", err)
		}
		if _, err := c.Exec(ctx, "DELETE FROM social.recent_searches WHERE searched_at<now() AND false"); err != nil {
			t.Fatalf("janitor recent-search cleanup: %v", err)
		}
		denied(t, execErr(c, ctx, "SELECT query FROM social.recent_searches LIMIT 1"), "janitor search-text read")
		denied(t, execErr(c, ctx, "SELECT body_cipher FROM chat.messages LIMIT 1"), "janitor message read")
		denied(t, execErr(c, ctx, "SELECT fix_cipher FROM private.user_locations LIMIT 1"), "janitor exact-fix read")
		denied(t, execErr(c, ctx, "SELECT id FROM identity.accounts LIMIT 1"), "janitor identity read")
		denied(t, execErr(c, ctx, "SELECT contact_id FROM safety.trusted_contacts LIMIT 1"), "janitor trusted-contact read")
		denied(t, execErr(c, ctx, "SELECT place_label FROM safety.checkins LIMIT 1"), "janitor checkin read")
		denied(t, execErr(c, ctx, "SELECT phone_cipher FROM identity.otp LIMIT 1"), "janitor otp cipher read")
		denied(t, execErr(c, ctx, "INSERT INTO identity.otp(phone_hash,phone_cipher,code_hash,expires_at) VALUES('x','x','x',now())"), "janitor otp write")
	})
}

func execErr(c *pgx.Conn, ctx context.Context, q string) error {
	_, err := c.Exec(ctx, q)
	return err
}
