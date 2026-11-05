BEGIN;
-- P0-1 fix: retention DELETE ... WHERE <column> and audit INSERT both failed
-- under 002's grants on a fresh database. PostgreSQL requires SELECT on
-- columns referenced in a DELETE's WHERE, and USAGE on a bigserial sequence
-- for INSERT. Column-scoped SELECT keeps the janitor unable to read
-- ciphertexts or message bodies; only the sweep predicates are reachable.

GRANT USAGE ON SEQUENCE safety.audit_id_seq TO nexus_moderator;

GRANT SELECT (expires_at, consumed) ON identity.otp TO nexus_janitor;
GRANT SELECT (expires_at) ON identity.sessions TO nexus_janitor;
GRANT SELECT (created_at) ON private.outbox TO nexus_janitor;
GRANT SELECT (expired_at) ON chat.tombstones TO nexus_janitor;

UPDATE identity.schema_version SET version = 4;
COMMIT;
