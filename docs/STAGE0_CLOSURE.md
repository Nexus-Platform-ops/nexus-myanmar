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

## Honest boundaries (unchanged)

- **P0-3 is at-rest encryption, not E2EE.** The API holds the key and can decrypt. The shared `DATA_KEY` is not KMS-envelope encryption.
- **DATA_KEY rotation is not supported yet.** There is no key-version column and no re-encryption migration; rotating the key breaks existing history. Documented in `docs/RUNBOOKS.md`.
- P0-1 still does not physically split social↔dating inside the API tier (monolith cross-schema joins require broad `nexus_app` DML); deferred to service extraction.
- P0-2 has no time-based Whisper expiry yet.

## Remaining P0

None in the Stage 0/S2 backlog; P0-4 closed with CI run 42175902801. External gates stay outside this backlog — independent pentest / security certification is an S4 gate, and moderation team staffing/SLA dispatch is an S4/S5 project-side gate. P0-4's own limits (brigade flag records suspicion rather than preventing the initial HIGH hold, thresholds are engineering defaults, appeals are CLI-only) are recorded in `evidence/p0-4-moderation.md`.
