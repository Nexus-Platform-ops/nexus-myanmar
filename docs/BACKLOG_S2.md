# S2 backlog — reconstructed P1/P2 (2026-11-05)

The external Stage 0 Report §5 P1/P2 item list is not present in this repository,
the roadmap export, evidence logs or git history. The list below is therefore
reconstructed from the documented release blockers in `docs/READINESS.md` and
the Stage 2 squad table in Roadmap v3. It is a working queue, not a ratified
product backlog; the roadmap line "P0 backlog ကုန်ပြီ — နောက် = P1×5/P2×6"
refers to it.

## P1 — correctness and safety gaps with existing schema support

| ID | Item | Source gap | Status |
|---|---|---|---|
| P1-1 | Whisper 24h time-based expiry, read-path hiding and janitor sweep with tombstone | READINESS 6 ("no time-based Whisper expiry") | CLOSED — CI 42184447461 (3bf55c0) |
| P1-2 | `Idempotency-Key` replay for create-post, share and report | READINESS 11 | CLOSED — CI 42184447461 (3bf55c0) |
| P1-3 | Symmetric friend graph API (`GET/POST/DELETE /v1/friends`) with block/ghost gates | READINESS G "Follow graph UX" | CLOSED — CI 42184447461 (3bf55c0) |
| P1-4 | Notification mute preferences; SAFETY cannot be muted | READINESS G "category mute settings" | CLOSED — CI 42184447461 (3bf55c0) |
| P1-5 | Search keyset pagination per type + recent-search API | READINESS C "cursor search, local recents UX" | CLOSED — CI 42184447461 (3bf55c0) |

## P2 — larger or infrastructure-dependent work

| ID | Item | Source gap | Status |
|---|---|---|---|
| P2-1 | Ordered WebSocket payloads / replay cursor across disconnects | READINESS 10, 11 | CLOSED — CI 42189122017 (a2cd122) |
| P2-2 | Meet-Safe trusted contacts and delayed check-in | READINESS D | CLOSED — CI 42189122017 (a2cd122) |
| P2-3 | DSR purge fan-out (30-day grace job, cross-store delete) | READINESS H, 12 | CLOSED — CI 42189122017 (a2cd122) |
| P2-4 | Media upload/purge/evidence escrow | READINESS B, F | open — vendor/residency decision in `evidence/p2-backlog.md` |
| P2-5 | Per-domain app roles / analytics isolation prep | READINESS 4 | CLOSED for aggregate analytics role — CI 42189122017 (a2cd122); per-domain app split deferred to service extraction |
| P2-6 | k6 staging load harness and p95 measurement scripts | READINESS 10 | open — install/target decision in `evidence/p2-backlog.md` |

P2 items are not claimed complete until each has its own evidence entry. Native
builds, SMS vendor integration, KMS/attestation and independent pentest remain
outside this queue (Stage 2 platform squads / Stage 4 external gate).

P1 closure evidence: `evidence/p1-backlog.md` (CI run 42184447461, commit 3bf55c0,
schema version 7). P2 closure evidence: `evidence/p2-backlog.md` (CI run
42189122017, commit a2cd122, schema version 11) for P2-1/P2-2/P2-3/P2-5; P2-4 and
P2-6 stay open with the vendor/staging decisions recorded there.
