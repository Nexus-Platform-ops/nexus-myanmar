# P0-1 — DB least-privilege roles (evidence log)

Date: 2026-11-05. Scope: physical grant split for the dual-profile isolation gap (roadmap v3 §17 blocker 1).

## Changes

- `db/migrations/002_privileges.sql` (new): ownership moved from bootstrap superuser to NOLOGIN `nexus_owner`; operational roles created NOLOGIN — `nexus_app` (DML on all 7 schemas, sequences, default privileges for future tables), `nexus_moderator` (UPDATE social.posts/comments/events + INSERT safety.audit), `nexus_janitor` (DELETE identity.otp/sessions, private.outbox, chat.tombstones), `nexus_readonly` (aggregate-safe columns on social/dating, no PII schemas); PUBLIC revoked on all schemas/tables/sequences; schema_version → 2.
- `cmd/migrate/main.go`: version ladder (0→1→2), idempotent re-run; activates operational roles via `ALTER ROLE ... WITH LOGIN PASSWORD $1` from env; fails closed when a password env is missing, <16 chars, or reused across roles.
- `internal/api/server.go`: readiness requires schema_version ≥ 2.
- `internal/api/server_test.go`: fixtures run on bootstrap connection; the server under test connects as `nexus_app` so integration proves the production grant shape.
- `internal/api/privileges_test.go` (new): `TestPrivilegeBoundaries` — app role cannot CREATE/ALTER/DROP/TRUNCATE/CREATE ROLE; moderator cannot read dating/location/identity/messages or write/delete social content; janitor cannot read message bodies, exact fixes, or identity, and cannot write OTP rows. Asserts SQLSTATE 42501.
- `compose.yaml`: api→nexus_app DSN; new `ops`-profile `janitor`/`moderate` services with scoped DSNs; migrate receives the three password envs.
- `.env.example`, `.github/workflows/verify.yml`: password envs + `TEST_APP_DATABASE_URL` for the integration lane.

## Verification

Local (this machine): `gofmt -l cmd internal` clean; `go vet ./...` clean; `go test -count=1 ./...` ok (unit, non-integration); `go build ./cmd/...` ok. Race detector and live Postgres integration not runnable here (no gcc/Docker) — same environment limits as Stage 0.

CI pending (project side): backend lane runs migrate + `TestIntegration` + `TestPrivilegeBoundaries` with race detector. Gate: both green + moderator/janitor denial assertions pass on Postgres 16/PostGIS.

## Honest boundary

This closes physical isolation for operational roles and removes DDL/owner power from application traffic. It does NOT physically split social↔dating inside the API tier (monolith cross-schema joins require broad `nexus_app` DML); that remains query-layer + privacy-test enforced and is deferred to service extraction. Recorded in `docs/READINESS.md` gap 4.
