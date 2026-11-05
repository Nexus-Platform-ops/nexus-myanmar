# P0-2 — Group Whisper per-recipient semantics (evidence log)

Date: 2026-11-05. Scope: roadmap v3 §17 blocker 2 — first reader wiped the whole room's Whisper messages, which would be false-deletion trust damage in group rooms.

## New semantics

- `chat.whisper_reads (message_id, reader, read_at)` records consumption per recipient (migration `003_whisper_reads.sql`, schema_version 3).
- `POST /v1/chats/{id}/read` marks all incoming Whispers ≤ `through_seq` consumed **for the caller only** (`ON CONFLICT DO NOTHING`), then physically deletes a message only when **every non-sender room member** has a read row. Response now includes `marked_read` alongside `wiped`.
- `GET messages` hides Whispers already consumed by the caller; the sender keeps seeing their own Whisper until all recipients have read it.
- 1:1 behavior is unchanged (single non-sender member ⇒ read triggers immediate physical wipe, tombstone + dedup-expired path identical).

## Changes

- `db/migrations/003_whisper_reads.sql` (new); `cmd/migrate` ladder extended to version 3; readiness requires version ≥ 3.
- `internal/api/chat.go`: read marking + conditional physical delete; history reader-filter.
- `internal/api/server_test.go`: new `group_whisper_per_recipient` integration scenario — first reader hides the message for themselves only (sender + unread member still see it, no physical wipe), last reader triggers the wipe for all, dedup replay reports `expired`.
- Disclosure copy updated (en/my/zh) to state deletion is per reader and room-wide only after each recipient reads.
- `docs/READINESS.md` item 6 rewritten to record the new semantics and remaining honest limits (no E2EE, backups not proven wiped).

## Verification

Local: `gofmt` clean, `go vet` clean, `go test -count=1 ./...` ok, `go build ./cmd/...` ok. Live integration (`group_whisper_per_recipient`, existing `mutual_match_and_whisper`) runs on the CI backend lane with race detector — pending project-side CI run, same environment limits as P0-1.

## Note

Unread Whisper rows now persist until every recipient reads them or a retention job appears; there is still no time-based Whisper expiry. `chat.whisper_reads` rows cascade-delete with their message. Flag for a future retention decision, not blocking this fix.
