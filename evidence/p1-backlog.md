# S2 P1 backlog — evidence log

Date: 2026-11-06. Scope: the five P1 items in `docs/BACKLOG_S2.md` (whisper expiry,
idempotency, friend graph, notification preferences, search pagination/recents).
The item list is reconstructed from `docs/READINESS.md` gaps plus the Roadmap v3
Stage 2 squad table; the external Stage 0 Report §5 is not in this repository.

## Changes

- `db/migrations/007_backlog_p1.sql` (new, schema_version → 7): `chat.messages.whisper_expires_at`
  with a partial index, `social.notification_prefs`, `social.recent_searches`, and the
  janitor grants needed for the new sweeps (`chat.messages` column SELECT + DELETE,
  `chat.tombstones` INSERT, `identity.idempotency` SELECT(created_at)/DELETE,
  `social.recent_searches` SELECT(searched_at)/DELETE).
- P1-1 — `internal/api/chat.go`: message reads hide expired or per-recipient-consumed
  whispers; sends set `whisper_expires_at = now() + 24h`; `readMessages` marks only
  unexpired whispers and tombstones expired/exhausted ones. `cmd/janitor` sweep:
  expired whispers → `chat.tombstones`, tombstones pruned after 30 days.
- P1-2 — `internal/api/idempotency.go` (new): `Idempotency-Key` replay on
  `POST /v1/posts`, `POST /v1/posts/{id}/shares` and `POST /v1/safety/report`;
  placeholder row (`status=0`) guards concurrent duplicates, only 2xx/3xx stored,
  body-hash mismatch → 409 `idempotency_key_reuse`, in-flight → 409
  `idempotency_in_progress`, replay sets `Idempotency-Replayed: true`.
- P1-3 — `internal/api/backlog.go`: symmetric `GET/POST/DELETE /v1/friends` over the
  existing `social.friendships` storage; self/invalid → 400, hour cap → 429,
  unverified/restricted/ghost target → 404, symmetric block → 403, duplicate add is
  a no-op that returns 201 and does not re-notify.
- P1-4 — `GET/PUT /v1/notifications/preferences`: ACTIVITY/FOLLOW mutes stored in
  `social.notification_prefs`; SAFETY is always retained (`safety_locked: true`).
- P1-5 — `GET /v1/search` keyset pagination per type (people `(real_name,user_id)`,
  posts `(created_at,id)` DESC, events `(starts_at,id)` ASC) with an opaque base64
  cursor and `LIMIT 21` lookahead; `GET/DELETE /v1/search/recent` keeps the last 20
  queries per user; janitor prunes recents after 30 days.
- Tests: five new integration subtests (`whisper_time_expiry`,
  `idempotency_key_replay`, `friend_graph_api`, `notification_preferences`,
  `search_pagination_and_recents`) plus janitor/idempotency/recents assertions in
  `TestPrivilegeBoundaries`.

## Verification

Local (this machine, 2026-11-05): `gofmt -l cmd internal` clean, `go vet ./...` clean,
`go test -count=1 ./...` ok, `go build ./cmd/...` ok. A CI-equivalent integration run
was also performed locally: migrations 001–007 applied to a scratch PostGIS database,
operational roles activated with the CI passwords, `miniredis` used for the Redis
rate limiter, and `go test -count=1 -run 'TestIntegration|TestPrivilegeBoundaries'`
passed twice (`ok nexus/internal/api 1.518s`, then `1.571s`). The race detector is
not runnable on this machine (no cgo/gcc), so CI remains the canonical tier.

## CI closure

GitHub Actions "Verify Nexus" run **42184447461** on commit **3bf55c0**
(`fix(s2): grant app DELETE on notification prefs`), 2026-11-06, conclusion
`success` — backend, mobile and graphql lanes all `success`.

- Backend applied the full ladder (`Applied schema version 1..7`, `Schema at
  version 7`) and ran the race-detector suite with `NEXUS_INTEGRATION=1`
  (live Postgres 16 + PostGIS + Redis), which includes the five new subtests and
  the extended privilege-boundary assertions.
- A first run (**42178920179**, commit c1e9598) failed `TestIntegration/notification_preferences`
  with `permission denied for table notification_prefs (SQLSTATE 42501)` because the
  test cleanup deletes as `nexus_app` while 007 granted only SELECT/INSERT/UPDATE.
  Fixed by granting DELETE in 007 (commit 3bf55c0) and re-run green.
- Local green was not treated as closure; only the CI run above closes the items.

**Status: P1-1 … P1-5 CLOSED (2026-11-06)** — evidence tier = CI run 42184447461,
commit 3bf55c0. Closure needs user ratification per the stage-gate rule.

## Honest boundary

- Idempotency covers three write endpoints, not the whole API. Replays are stored in
  `identity.idempotency` for 24h; a retry after prune is treated as a new request.
- The 24h Whisper TTL is a product default, not a policy validated with users; expiry
  is enforced on read and by the janitor sweep, so an expired row can exist briefly
  but is never returned.
- Friend adds are immediate and silent beyond one FOLLOW notification — there is no
  consent/accept step, and no friend-request inbox.
- Search pagination is exact within one data snapshot only; rows inserted between
  pages can be missed or duplicated, which is normal keyset behaviour.
- Notification preferences cover ACTIVITY/FOLLOW only; SAFETY delivery is
  intentionally not user-mutable.
