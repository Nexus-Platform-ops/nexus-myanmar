BEGIN;
-- P0-4: moderation workflow + abuse controls.
-- Named reviewers make every decision attributable; appeals give users a
-- review path; brigade_flags records coordinated reporting for human review.
CREATE TABLE safety.reviewers (
  reviewer_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL UNIQUE CHECK (length(name) BETWEEN 3 AND 80),
  role text NOT NULL CHECK (role IN ('MODERATOR','SENIOR')),
  password_hash text NOT NULL,
  active boolean NOT NULL DEFAULT true,
  decisions_count int NOT NULL DEFAULT 0 CHECK (decisions_count >= 0),
  created_at timestamptz NOT NULL DEFAULT now());

CREATE TABLE safety.appeals (
  appeal_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
  decision_id bigint NOT NULL REFERENCES safety.audit ON DELETE CASCADE,
  reason text NOT NULL CHECK (length(reason) BETWEEN 10 AND 2000),
  status text NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','UPHELD','OVERTURNED')),
  reviewed_by uuid REFERENCES safety.reviewers,
  reviewed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK ((status = 'OPEN') = (reviewed_by IS NULL AND reviewed_at IS NULL)));
CREATE UNIQUE INDEX safety_appeals_one_per_decision ON safety.appeals(decision_id);
CREATE INDEX safety_appeals_owner ON safety.appeals(user_id, created_at DESC);

CREATE TABLE safety.brigade_flags (
  target_id uuid PRIMARY KEY REFERENCES identity.accounts ON DELETE CASCADE,
  reporters int NOT NULL CHECK (reporters > 0),
  reason text NOT NULL,
  flagged_at timestamptz NOT NULL DEFAULT now());

ALTER TABLE safety.reviewers OWNER TO nexus_owner;
ALTER TABLE safety.appeals OWNER TO nexus_owner;
ALTER TABLE safety.brigade_flags OWNER TO nexus_owner;

-- API: submit and list the caller's own appeals; record brigade flags.
GRANT SELECT, INSERT ON safety.appeals TO nexus_app;
GRANT SELECT, INSERT, UPDATE ON safety.brigade_flags TO nexus_app;

-- Moderator: authenticate against reviewer rows, review appeals, and read the
-- decisions they reference. Column SELECT on social content is needed for the
-- UPDATE ... WHERE id predicates; status UPDATE already exists from 002.
GRANT SELECT ON safety.reviewers TO nexus_moderator;
GRANT UPDATE (decisions_count) ON safety.reviewers TO nexus_moderator;
GRANT SELECT, UPDATE ON safety.appeals TO nexus_moderator;
GRANT SELECT ON safety.audit TO nexus_moderator;
GRANT SELECT (id) ON social.posts, social.comments, social.events TO nexus_moderator;

UPDATE identity.schema_version SET version = 6;
COMMIT;
