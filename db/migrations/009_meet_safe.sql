BEGIN;
-- P2-2: Meet-Safe. A user names trusted contacts (existing accounts) and can
-- schedule a check-in before meeting someone; the contacts see an overdue
-- check-in and the owner confirms or cancels it. Push delivery does not exist,
-- so overdue state is derived at read time rather than pushed.
CREATE TABLE safety.trusted_contacts (
 user_id uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
 contact_id uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
 label text NOT NULL CHECK(length(label) BETWEEN 1 AND 40),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,contact_id),
 CHECK(user_id<>contact_id));

CREATE TABLE safety.checkins (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
 event_id uuid REFERENCES social.events ON DELETE SET NULL,
 place_label text NOT NULL CHECK(length(place_label) BETWEEN 1 AND 80),
 due_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'PENDING' CHECK(status IN ('PENDING','CONFIRMED','CANCELLED')),
 created_at timestamptz NOT NULL DEFAULT now(),
 resolved_at timestamptz,
 CHECK((status='PENDING')=(resolved_at IS NULL)));
CREATE INDEX ON safety.checkins(user_id,created_at DESC);
CREATE INDEX ON safety.checkins(due_at) WHERE status='PENDING';

ALTER TABLE safety.trusted_contacts OWNER TO nexus_owner;
ALTER TABLE safety.checkins OWNER TO nexus_owner;

GRANT SELECT, INSERT, UPDATE, DELETE ON safety.trusted_contacts, safety.checkins TO nexus_app;

UPDATE identity.schema_version SET version = 9;
COMMIT;
