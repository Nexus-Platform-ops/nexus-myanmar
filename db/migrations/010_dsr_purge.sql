BEGIN;
-- P2-3: DSR grace period. Deletion is only requested here; cmd/purge fans the
-- erase out across stores after 30 days so a mistaken request stays reversible
-- and shared conversation state is not destroyed inline.
CREATE TABLE identity.deletion_requests (
 user_id uuid PRIMARY KEY REFERENCES identity.accounts ON DELETE CASCADE,
 requested_at timestamptz NOT NULL DEFAULT now(),
 purge_after timestamptz NOT NULL DEFAULT now()+interval '30 days',
 CHECK(purge_after>requested_at));
CREATE INDEX ON identity.deletion_requests(purge_after);

ALTER TABLE identity.deletion_requests OWNER TO nexus_owner;
GRANT SELECT, INSERT, UPDATE, DELETE ON identity.deletion_requests TO nexus_app;

UPDATE identity.schema_version SET version = 10;
COMMIT;
