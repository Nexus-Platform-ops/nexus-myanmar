# S2 P2 backlog — evidence log

Date: 2026-11-06. Scope: the four implemented P2 items in `docs/BACKLOG_S2.md`
(ordered WebSocket replay, Meet-Safe, DSR grace/purge, analytics isolation).
P2-4 (media escrow) and P2-6 (k6 staging harness) remain open — see the decision
section at the end.

## Changes

- P2-1 — `db/migrations/008_realtime_outbox.sql` (schema_version → 8): index on
  `private.outbox(user_id,id)`. `internal/api/chat.go`: `GET /v1/ws?cursor=N`
  resumes a client after the last applied outbox id; a fresh connection starts at
  the current head; drain reads `ORDER BY id LIMIT 100` batches and sends
  `{"type":"event","id",...}` frames; a cursor older than the retained minimum
  emits `replay_truncated`; heartbeats carry the current cursor. The previous
  timestamp-based `invalidate` poll is gone.
- P2-2 — `db/migrations/009_meet_safe.sql` (schema_version → 9):
  `safety.trusted_contacts` (max 10 per owner, self-reference forbidden) and
  `safety.checkins` (PENDING/CONFIRMED/CANCELLED, `due_at` index). New routes in
  `internal/api/meetsafe.go`: `GET/POST/DELETE /v1/safety/trusted-contacts`,
  `GET/POST /v1/safety/checkins`, `POST /v1/safety/checkins/{id}/confirm|cancel`.
  Adds are gated by write eligibility, block state, and ghost/restricted targets;
  trusted contacts are notified through SAFETY. Overdue state is computed at read
  time (`due_at<=now()`), since push delivery does not exist.
- P2-3 — `db/migrations/010_dsr_purge.sql` (schema_version → 10):
  `identity.deletion_requests` with a 30-day `purge_after`. `DELETE /v1/account`
  now records the request, revokes every other session, ghosts the account and
  returns 202; `GET /v1/account/deletion` and `POST /v1/account/deletion/cancel`
  expose and reverse it; `mayWrite` and OTP verification refuse a pending account.
  `internal/retention/purge.go` + `cmd/purge` erase up to 100 due accounts per
  run in one transaction (`FOR UPDATE SKIP LOCKED`), including OTP rows by phone
  hash, outbox rows, FK-cascaded data and rooms left with fewer than two members.
  `compose.yaml` adds the `ops`-profile `purge` service; `docs/RUNBOOKS.md`
  documents the owner-DSN run.
- P2-5 — `db/migrations/011_analytics.sql` (schema_version → 11): NOLOGIN role
  `nexus_analytics` with USAGE on schema `analytics` and SELECT on five
  security-definer aggregate views (post/event/report volume suppress cells below
  k=5; message and session volume have no per-user dimension). The role has no
  grant on any base schema. `cmd/migrate` activates the role from
  `NEXUS_ANALYTICS_DB_PASSWORD`; CI provides the credential.

## Tests

- `websocket_ordered_replay` — cursor resume, out-of-order insert visibility,
  `replay_truncated` for a pruned cursor, live events after drain.
- `meet_safe_trusted_contacts` — validation, ghost target 404, label upsert,
  block filtering, delete idempotency.
- `meet_safe_checkin_flow` — input validation, open check-in limit, contact-side
  visibility, owner-only confirm, overdue derivation, notification.
- `account_deletion_grace_and_purge` / `account_delete_grace` — 202/grace record,
  write refusal, session revocation, cancel path, purge of due accounts only.
- `analytics_aggregate_only` (privilege suite) — all five views readable by
  `nexus_analytics`; denied on base-schema PII/content columns and DDL.

## Verification

Local (2026-11-05/06): `gofmt -l cmd internal` clean, `go vet ./...` clean,
`go test -count=1 ./...` ok, `go build ./cmd/...` ok. The race detector is not
runnable on this machine (no cgo/gcc); CI is the canonical tier.

## CI closure

GitHub Actions "Verify Nexus" run **42189122017** on commit **a2cd122**
(`feat(s2): Meet-Safe trusted contacts and check-ins (P2-2)`), 2026-11-06,
conclusion `success` — backend, mobile and graphql jobs all `success`.

The run head contains all four P2 commits: `b2ce6f6` (P2-1), `381b891` (P2-3),
`f702204` (P2-5), `a2cd122` (P2-2). Backend applied the migration ladder through
version 11 (`Schema at version 11`) and ran the race-detector integration suite
with `NEXUS_INTEGRATION=1`, which includes the subtests above and the extended
privilege-boundary checks. Local green was not treated as closure; only the CI
run above closes the items.

**Status: P2-1, P2-2, P2-3, P2-5 CLOSED (2026-11-06)** — evidence tier = CI run
42189122017, commit a2cd122, schema version 11. Closure needs user ratification
per the stage-gate rule.

## Honest boundaries

- WebSocket replay is at-least-once and id-ordered, not exactly-once. Outbox ids
  are assigned at INSERT, so a transaction that commits after a higher id was
  already delivered can be missed until it refetches; the client cursor is the
  only resume contract. Outbox rows are pruned after 24h, so older cursors get
  `replay_truncated` and must refetch. The wire format changed from invalidation
  hints to event payloads — older clients are not compatible.
- Meet-Safe contacts are existing accounts only; there is no invitation by phone
  or SMS, no push/OS delivery, and the contact sees overdue state only when the
  app reads `GET /v1/safety/checkins?role=contact`. Limits are 10 contacts and 5
  open check-ins per user. No location is shared with contacts.
- DSR purge covers hot database rows only. Backups/WAL/PITR, evidence escrow,
  media objects and legal retention rules are outside the job and remain an
  S4/legal gate. Purging is batch-limited to 100 accounts per run and requires
  the owner-level DSN. Cancelling restores `ghost=false`, so an account that was
  already ghosted before the request becomes visible again.
- The analytics role is aggregate-only, not a warehouse: there is no retention
  policy, no per-domain app-role split (deferred to service extraction), and the
  views are security-definer objects owned by `nexus_owner`, so their definitions
  need review before exposure. `nexus_readonly` still exists for other uses.
- P2-4 (media upload/purge/evidence escrow) and P2-6 (k6 staging harness) are
  **not** implemented; their vendor/staging decisions are tracked separately.

## P2-4 / P2-6 decisions (pending ratification)

- P2-4 media: use S3-compatible object storage in `ap-southeast-1` for primary
  storage, with a local (on-prem) mirror for evidence escrow; uploads are
  pre-signed, server-side encrypted, content-type pinned, size-capped and
  virus-scanned before publish. Purge runs as a scheduled job with a tombstone
  audit row. This is a plan, not evidence — no bucket or credential exists yet.
- P2-6 load: install k6 (pinned release binary) in CI/staging runners, not on
  developer laptops; target the staging stack (Compose/DB/Redis + API) with the
  existing 15K-WS/300K-DAU profile, publish p95 and error rate against an agreed
  budget. No staging environment or load results exist yet.
