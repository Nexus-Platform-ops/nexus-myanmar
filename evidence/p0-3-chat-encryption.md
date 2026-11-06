# P0-3 — Chat bodies encrypted at rest (evidence log)

Date: 2026-11-05. Scope: production pre-condition — `chat.messages.body` was stored as plaintext (READINESS gap 6 / module F), so database files, dumps and backups exposed every normal message body. This adds server-side at-rest encryption; it is **not** E2EE.

## Changes

- `db/migrations/005_encrypt_chat.sql` (new, schema_version → 5): adds `body_cipher bytea`, backfills every existing body with `pgp_sym_encrypt(body, current_setting('nexus.data_key'))`, sets NOT NULL, drops the plaintext column. Same pgcrypto envelope already used for OTP phones and exact fixes.
- `cmd/migrate/main.go`: ladder extended to version 5; sets the `nexus.data_key` session GUC from `DATA_KEY` before applying 005 and fails closed when it is missing or <32 bytes, so a keyless run cannot silently store plaintext.
- `internal/api/chat.go`: send encrypts the body on INSERT (`pgp_sym_encrypt($4,$5)`); history decrypts on SELECT (`pgp_sym_decrypt(body_cipher,$4)`). Dedup replay and Whisper deletion paths are unchanged (they do not read bodies).
- `internal/api/safety.go`: account export decrypts the subject's own messages before returning them.
- `internal/api/server.go`: readiness requires schema_version ≥ 5.
- `internal/api/server_test.go`: integration asserts the stored `body_cipher` does not contain the plaintext and decrypts back to the original text; privileged roles still cannot read the column.
- `internal/api/privileges_test.go`: denial probes updated to the new column.
- `.github/workflows/verify.yml`: `DATA_KEY` supplied to the backend lane so migrate can apply 005.

## Verification

Local scratch Postgres 16 (data-migration path, which a fresh CI database cannot exercise): migration applied with the key GUC — one legacy plaintext row became a 79-byte ciphertext containing no plaintext bytes and decrypted back to `legacy secret`; `body` column removed, `body_cipher bytea NOT NULL`; running 005 without the GUC failed closed (`unrecognized configuration parameter "nexus.data_key"`) and rolled back. `gofmt` clean, `go vet` clean, `go build ./...` ok.

CI closure: GitHub Actions "Verify Nexus" run 42174109707 (commit 2980171) — backend, mobile and graphql lanes all `success`. Backend ran migrate v1–5 with the `DATA_KEY` GUC (`Applied schema version 1..5`, `Schema at version 5`), the race-detector integration suite (`ok nexus/internal/api 1.701s`) including the message round-trip plus at-rest ciphertext assertion and the privilege-boundary denials on `body_cipher`.

Status: **CLOSED (2026-11-05)** — evidence tier = CI run 42174109707.

## Honest boundary

This protects data at rest under a server-held key. It is not E2EE: the API can decrypt, and the shared `DATA_KEY` is not KMS-envelope encryption. There is no key-version column and no automatic re-encryption on rotation (see docs/RUNBOOKS.md). Whisper expiry, backups, WAL and device caches remain as documented in READINESS gap 6.
