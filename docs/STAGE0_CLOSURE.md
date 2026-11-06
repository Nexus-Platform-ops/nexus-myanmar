# Stage 0 closure record — P0 blockers

Date: 2026-11-05. This is the repo-side closure record for the Stage 0 / S2 P0 backlog (the external Stage 0 Report is not part of the repo; this file is referenced from roadmap v3 §17).

## Closed items

| ID | Item | Evidence tier | Status |
|---|---|---|---|
| P0-1 | Cross-schema DB role (least-privilege split) | CI run 42101894966, commit b29a749 | CLOSED 2026-11-05 |
| P0-2 | Group Whisper first-read-kills-room | CI run 42101894966, commit b29a749 | CLOSED 2026-11-05 |
| P0-3 | Chat message bodies stored in plaintext at rest | CI run 42174109707, commit 2980171 | CLOSED 2026-11-05 |
| P0-4 | Anonymous moderation + no appeal path + report abuse | CI run 42175902801, commit 2569230 | CLOSED 2026-11-05 |

### P0-3 evidence detail

Run 42174109707 ("Verify Nexus") completed `success` for the backend, mobile and graphql lanes. Backend applied the migration ladder v1–5 with the `DATA_KEY` session GUC (`Applied schema version 1..5`, `Schema at version 5`), then ran the race-detector integration suite (`ok nexus/internal/api 1.701s`), which includes the message round-trip through the API plus the at-rest assertion that `chat.messages.body_cipher` contains no plaintext bytes, and the privilege-boundary denials on the new column. Evidence log: `evidence/p0-3-chat-encryption.md`.

### P0-4 evidence detail

Run 42175902801 completed `success` for all three lanes. Backend applied the ladder through version 6 and ran the live race-detector suite (`ok nexus/internal/api 7.113s` with the integration flag set). The new reviewer/appeal subtest and the `report_daily_cap`, `coordinated_report_flag` and `appeal_workflow` subtests are part of that suite; the privilege-boundary test adds app/moderator checks on `safety.reviewers`. Evidence log: `evidence/p0-4-moderation.md`.

## S2 P1 closure

| ID | Item | Evidence tier | Status |
|---|---|---|---|
| P1-1 | Whisper 24h time-based expiry + janitor sweep | CI run 42184447461, commit 3bf55c0 | CLOSED 2026-11-06 |
| P1-2 | `Idempotency-Key` replay (post/share/report) | CI run 42184447461, commit 3bf55c0 | CLOSED 2026-11-06 |
| P1-3 | Friend graph API | CI run 42184447461, commit 3bf55c0 | CLOSED 2026-11-06 |
| P1-4 | Notification mute preferences (SAFETY locked) | CI run 42184447461, commit 3bf55c0 | CLOSED 2026-11-06 |
| P1-5 | Search cursor pagination + recent searches | CI run 42184447461, commit 3bf55c0 | CLOSED 2026-11-06 |

Run 42184447461 ("Verify Nexus") completed `success` for backend, mobile and graphql.
Backend applied the ladder through version 7 and ran the live race-detector suite,
which includes the five new subtests plus the extended janitor assertions. The
preceding run 42178920179 (commit c1e9598) failed the new notification-preference
subtest on a missing app-role DELETE grant; that was the single blocking defect and
was fixed in 3bf55c0 before closure. Evidence log: `evidence/p1-backlog.md`.
Moderation and privilege logs carry a P1 regression section for the same run.

## S2 P2 closure

| ID | Item | Evidence tier | Status |
|---|---|---|---|
| P2-1 | Ordered WebSocket payloads / replay cursor | CI run 42189122017, commit a2cd122 | CLOSED 2026-11-06 |
| P2-2 | Meet-Safe trusted contacts + check-ins | CI run 42189122017, commit a2cd122 | CLOSED 2026-11-06 |
| P2-3 | DSR 30-day grace + scheduled cross-store purge | CI run 42189122017, commit a2cd122 | CLOSED 2026-11-06 |
| P2-5 | Aggregate-only analytics role | CI run 42189122017, commit a2cd122 | CLOSED 2026-11-06 (per-domain app split deferred) |
| P2-4 | Media upload/purge/evidence escrow | — | open (vendor/residency decision recorded) |
| P2-6 | k6 staging load harness | — | open (install/target decision recorded) |

Run 42189122017 ("Verify Nexus") completed `success` for backend, mobile and
graphql on head commit a2cd122, which contains all four P2 commits (`b2ce6f6`,
`381b891`, `f702204`, `a2cd122`). Backend applied the ladder through version 11
and ran the live race-detector suite, including `websocket_ordered_replay`,
`meet_safe_trusted_contacts`, `meet_safe_checkin_flow`,
`account_deletion_grace_and_purge` and `analytics_aggregate_only`. Evidence log:
`evidence/p2-backlog.md`.

## S2 coverage gate closure

| ID | Item | Evidence tier | Status |
|---|---|---|---|
| R1 | Go API statement coverage >=80% (`nexus/internal/api`) | CI run 42203777688, commit ba1108f | CLOSED 2026-11-06 |
| R2 | Flutter line coverage >=80% (`apps/mobile/lib`) | CI run 42203777688, commit ba1108f | CLOSED 2026-11-06 |

Run 42203777688 ("Verify Nexus") completed `success` for backend, mobile and
graphql on commit `ba1108f` (backend 54s, mobile 1m45s, graphql 8s). The two
gate steps are fail-closed awk thresholds over the race-step `coverage.out` and
`lcov.info`: Go API 83.63% (1492/1784) and Flutter 95.88% (1325/1382), both
measured with the CI toolchain.

Two earlier runs failed and are recorded rather than hidden: run 42198661290
failed at the old gate step, which re-ran the integration suite; run
42202118487 then failed with `API coverage 79.54% < 80%` because the local
80.5% (1512/1879) came from Go 1.27.1 while CI pins Go 1.25.5. Commit `ba1108f`
adds the malformed-body/read-only/room-and-geo coverage sweeps that close the
gap. Evidence log: `evidence/coverage-gate.md`.

## Honest boundaries (unchanged)

- **P0-3 is at-rest encryption, not E2EE.** The API holds the key and can decrypt. The shared `DATA_KEY` is not KMS-envelope encryption.
- **DATA_KEY rotation is not supported yet.** There is no key-version column and no re-encryption migration; rotating the key breaks existing history. Documented in `docs/RUNBOOKS.md`.
- P0-1 still does not physically split social↔dating inside the API tier (monolith cross-schema joins require broad `nexus_app` DML); deferred to service extraction.
- P0-2's no-time-expiry limitation is now addressed by P1-1 (24h TTL + janitor tombstone), closed with CI run 42184447461.
- **P2-1 replay is at-least-once, not exactly-once.** Outbox ids are assigned at INSERT, so a late-committing transaction can be missed until the client refetches; the 24h outbox window means old cursors get `replay_truncated`. The WS wire format changed from invalidation hints to payload frames.
- **P2-3 erases hot database rows only.** Backups/WAL/PITR, evidence escrow, media objects and legal retention remain an S4/legal gate; the job needs the owner DSN and is batch-limited (100 accounts/run).
- **P2-5 is an analytics role, not a warehouse.** The five security-definer views are aggregate-only (k>=5 for township/category cells) and owned by `nexus_owner`; per-domain application roles are still deferred to service extraction.

## Remaining P0

None in the Stage 0/S2 backlog; P0-4 closed with CI run 42175902801 and the P1 queue closed with CI run 42184447461. Four of the six P2 items (P2-1, P2-2, P2-3, P2-5) closed with CI run 42189122017; P2-4 (media) and P2-6 (k6 staging) stay open and are not claimed. The S2 coverage gate closed with CI run 42203777688 (commit ba1108f). External gates stay outside this backlog — independent pentest / security certification is an S4 gate, and moderation team staffing/SLA dispatch is an S4/S5 project-side gate. P0-4's own limits (brigade flag records suspicion rather than preventing the initial HIGH hold, thresholds are engineering defaults, appeals are CLI-only) are recorded in `evidence/p0-4-moderation.md`; P1 limits are in `evidence/p1-backlog.md`; P2 limits are in `evidence/p2-backlog.md`.
