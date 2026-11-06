# P0-4 — Moderation workflow + abuse controls (evidence log)

Date: 2026-11-05. Scope: beta precondition — moderation decisions were anonymous DB updates made by a trusted operator CLI, users had no appeal path, and report volume had no daily cap or coordinated-abuse detection. External gates stay out of scope: independent pentest/security certification is S4, and moderation team staffing/SLA dispatch is S4/S5 project-side work.

## Changes

### Named reviewer auth (P0-4a)

- `db/migrations/006_moderation.sql` (new, schema_version → 6): `safety.reviewers(reviewer_id, name UNIQUE, role MODERATOR|SENIOR, password_hash, active, decisions_count, created_at)` owned by `nexus_owner`.
- `internal/moderation` (new package): bcrypt `Authenticate` (inactive and wrong-password both return the same error), `CreateReviewer` (operator-admin only), `SetReviewerActive`, `Decide`.
- `cmd/moderate`: `post|comment|event` decisions now require `REVIEWER_NAME` + `REVIEWER_PASSWORD`; `reviewer add|enable|disable` uses a separate `ADMIN_DATABASE_URL`; decisions print the reviewer and remaining rotation budget. `safety.audit.actor` is the authenticated reviewer_id for every decision.
- Least privilege: `nexus_moderator` may read `safety.reviewers`, update only `decisions_count`, read `safety.audit`, and select content `id` for UPDATE predicates. It cannot insert reviewers or change password hashes; `nexus_app` cannot read reviewers at all.

### Appeals workflow (P0-4b)

- `safety.appeals(appeal_id, user_id, decision_id → safety.audit, reason, status OPEN|UPHELD|OVERTURNED, reviewed_by, reviewed_at)` with one appeal per decision and a status/timestamp consistency check.
- `POST /v1/safety/appeals` accepts only `MODERATE_*_REMOVED` decisions whose resource the caller owns (post author / comment author / event host); duplicate → 409, non-owner or non-removal → 404. `GET /v1/safety/appeals` lists only the caller's appeals without exposing reviewer identity.
- `moderate appeal APPEAL_UUID UPHELD|OVERTURNED`: refuses self-review (the original decision's actor cannot review it), refuses non-open appeals, records `reviewed_by`/`reviewed_at`, audits `APPEAL_UPHELD`/`APPEAL_OVERTURNED`, and restores an overturned REMOVED resource to LIVE.

### Anti-brigading (P0-4c)

- Reporter cap: 5 reports per UTC day per user (`report_daily_limit`, 429) in addition to the existing 10/hour cap.
- Reviewer rotation: `MaxDecisionsPerReviewer = 100`; the slot is claimed with a guarded `UPDATE ... WHERE active AND decisions_count < 100`, so the cap holds under concurrency and the decision transaction rolls back if the review target is invalid.
- Coordinated reporting: when five distinct reporters target one account within 24h, `safety.brigade_flags` records the count and a `BRIGADE_SUSPECTED` audit row. Volume alone never changes content state; it flags for human review.

### Wiring

- `cmd/migrate` ladder extended to 006; `GET /health/ready` requires schema version ≥ 6.
- `compose.yaml`/`.env.example` expose `ADMIN_DATABASE_URL` and reviewer env names; `docs/RUNBOOKS.md` documents provisioning, rotation and the no-delete rule for audit attribution.

## Verification

Local (this machine, no Docker/PostGIS): `gofmt -l cmd internal` clean, `go vet ./...` clean, `go build ./...` clean, `go test -count=1 ./...` ok (integration tests skipped without `NEXUS_INTEGRATION=1`). No local live-Postgres run of 006 was performed; CI is the evidence tier.

CI pending: backend lane applies migrate v1–6, then runs with the race detector:
- `TestModerationIntegration` (new): reviewer creation audited, wrong password and inactive reviewer rejected, decision attributed to reviewer_id, self-review rejected, overturn restores LIVE and records reviewer, closed appeal rejected, rotation cap enforced and reset.
- `TestIntegration` new subtests: `report_daily_cap`, `coordinated_report_flag`, `appeal_workflow` (ownership, duplicate, validation, list).
- `TestPrivilegeBoundaries`: app cannot read reviewers; moderator can read reviewers/update the counter but cannot create reviewers or write password hashes.

Gate: all lanes green before P0-4 is marked CLOSED.

## Honest boundary

- A brigade flag records suspicion only. The first HIGH report still auto-holds exposure, so coordinated reporting can cause a temporary hold until a reviewer overturns it; staffed response remains required.
- Thresholds (5/day, 100 decisions, 5 distinct reporters) are engineering defaults, not independently validated abuse-policy values.
- Appeal review is CLI-only; there is no reviewer web UI or notification to the appellant. Appeal/audit retention policy and protected media escrow are not implemented.
- Bcrypt hashes and reviewer passwords are provisioned through environment variables today; KMS-backed secrets and independent review are S4 gates.
