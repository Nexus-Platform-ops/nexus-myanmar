# P0-1 — DB least-privilege roles (evidence log)

Date: 2026-11-05. Scope: physical grant split for the dual-profile isolation gap (roadmap v3 §17 blocker 1).

## Changes

- `db/migrations/002_privileges.sql` (new): ownership moved from bootstrap superuser to NOLOGIN `nexus_owner`; operational roles created NOLOGIN — `nexus_app` (DML on all 7 schemas, sequences, default privileges for future tables), `nexus_moderator` (UPDATE social.posts/comments/events + INSERT safety.audit), `nexus_janitor` (DELETE identity.otp/sessions, private.outbox, chat.tombstones), `nexus_readonly` (aggregate-safe columns on social/dating, no PII schemas); PUBLIC revoked on all schemas/tables/sequences; schema_version → 2 (final ladder 0→4, see 004 note below).
- `cmd/migrate/main.go`: version ladder (0→4), idempotent re-run; activates operational roles via `ALTER ROLE ... WITH LOGIN PASSWORD` from env (quoted literal — utility statements reject bind params); fails closed when a password env is missing, <16 chars, or reused across roles.
- `internal/api/server.go`: readiness requires schema_version ≥ 4.
- `internal/api/server_test.go`: fixtures run on bootstrap connection; the server under test connects as `nexus_app` so integration proves the production grant shape.
- `internal/api/privileges_test.go` (new): `TestPrivilegeBoundaries` — app role cannot CREATE/ALTER/DROP/TRUNCATE/CREATE ROLE; moderator cannot read dating/location/identity/messages or write/delete social content; janitor cannot read message bodies, exact fixes, or identity, and cannot write OTP rows. Asserts SQLSTATE 42501.
- `compose.yaml`: api→nexus_app DSN; new `ops`-profile `janitor`/`moderate` services with scoped DSNs; migrate receives the three password envs.
- `.env.example`, `.github/workflows/verify.yml`: password envs + `TEST_APP_DATABASE_URL` for the integration lane.
- `db/migrations/004_grants_fix.sql` (new, schema_version → 4): column-scoped SELECT for janitor sweep predicates (expires_at/consumed/created_at/expired_at), `safety.audit_id_seq` USAGE for moderator. Found by CI run 42100241341; semantics verified on a local scratch database before re-push.

## Verification

Local (this machine): `gofmt -l cmd internal` clean; `go vet ./...` clean; `go test -count=1 ./...` ok (unit, non-integration); `go build ./cmd/...` ok. Race detector and live Postgres integration not runnable here (no gcc/Docker) — same environment limits as Stage 0.

CI closure (designated evidence tier — GitHub Actions run 42101894966 on commit b29a749, 2026-11-05, all jobs success):

- Backend lane migrate: `Applied schema version 1/2/3/4` → `Schema at version 4` (002 + 004 grants; role activation fail-closed from env succeeded).
- `TestPrivilegeBoundaries` under race detector, `NEXUS_INTEGRATION=1`: `app_has_no_ddl`, `moderator_scoped`, `janitor_scoped` all PASS — denial assertions on dating/location/identity/messages/social writes, message bodies, exact fixes, OTP ciphertext all hold on Postgres 16 + PostGIS.
- Note: two grant bugs were found by the first CI run (42100241341) and fixed in `004_grants_fix.sql` before closure — PostgreSQL requires SELECT on columns referenced in a DELETE WHERE (janitor sweeps) and USAGE on a bigserial sequence (moderator audit insert). Column-scoped SELECTs keep ciphertext/body reads denied (asserted in the same run).

**Status: CLOSED (2026-11-05).**

### Grant regression check — S2 P1 (2026-11-06)

CI run 42184447461 (commit 3bf55c0, all lanes `success`) applied schema version 7
on top of the v4 grant shape. `007_backlog_p1.sql` adds only additive grants —
`nexus_app` DML on `social.notification_prefs`/`social.recent_searches`,
`nexus_janitor` column-scoped SELECT + DELETE on `chat.messages`/`identity.idempotency`/
`social.recent_searches` and INSERT on `chat.tombstones` — and does not widen any
existing boundary. `TestPrivilegeBoundaries` (race detector, live Postgres, same run)
kept every denial assertion green, including the new ones: janitor can run the
whisper sweep, idempotency and recents pruning, but `SELECT query FROM
social.recent_searches` is still denied with SQLSTATE 42501 (janitor may only read
`searched_at` for the sweep predicate).

The prior run 42178920179 (commit c1e9598) caught one missing grant —
`permission denied for table notification_prefs (SQLSTATE 42501)` on the app-role test
cleanup — which is the same class of bug 004 fixed; corrected in 3bf55c0 by
granting DELETE to `nexus_app` on that table only.

## Honest boundary

This closes physical isolation for operational roles and removes DDL/owner power from application traffic. It does NOT physically split social↔dating inside the API tier (monolith cross-schema joins require broad `nexus_app` DML); that remains query-layer + privacy-test enforced and is deferred to service extraction. Recorded in `docs/READINESS.md` gap 4.
