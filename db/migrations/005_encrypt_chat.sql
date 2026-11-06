BEGIN;
-- P0-3: chat bodies were stored as plaintext. Encrypt at rest with the same
-- pgcrypto envelope already used for OTP phones and exact fixes. The DataKey is
-- supplied by cmd/migrate as the session GUC nexus.data_key, so existing rows
-- are encrypted in place under the same key the API will read them with.
ALTER TABLE chat.messages ADD COLUMN body_cipher bytea;
UPDATE chat.messages SET body_cipher = pgp_sym_encrypt(body, current_setting('nexus.data_key'));
ALTER TABLE chat.messages ALTER COLUMN body_cipher SET NOT NULL;
ALTER TABLE chat.messages DROP COLUMN body;
UPDATE identity.schema_version SET version = 5;
COMMIT;
