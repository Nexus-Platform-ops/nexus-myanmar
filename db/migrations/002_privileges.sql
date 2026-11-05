BEGIN;
-- P0-1: physical least-privilege split. Bootstrap superuser owns nothing after this.
-- Operational roles: nexus_app (API DML), nexus_moderator (content decisions),
-- nexus_janitor (retention), nexus_readonly (analytics, no PII). Passwords are
-- assigned by cmd/migrate from env; roles stay NOLOGIN until then (fail-closed).

CREATE ROLE nexus_owner NOLOGIN;
CREATE ROLE nexus_app NOLOGIN;
CREATE ROLE nexus_moderator NOLOGIN;
CREATE ROLE nexus_janitor NOLOGIN;
CREATE ROLE nexus_readonly NOLOGIN;

DO $$
DECLARE s text; t text;
BEGIN
  FOREACH s IN ARRAY ARRAY['identity','social','dating','private','safety','chat'] LOOP
    EXECUTE format('ALTER SCHEMA %I OWNER TO nexus_owner', s);
    FOR t IN SELECT tablename FROM pg_tables WHERE schemaname = s LOOP
      EXECUTE format('ALTER TABLE %I.%I OWNER TO nexus_owner', s, t);
    END LOOP;
    FOR t IN SELECT sequencename FROM pg_sequences WHERE schemaname = s LOOP
      EXECUTE format('ALTER SEQUENCE %I.%I OWNER TO nexus_owner', s, t);
    END LOOP;
  END LOOP;
END $$;

REVOKE ALL ON SCHEMA identity, social, dating, private, safety, chat FROM PUBLIC;
REVOKE ALL ON ALL TABLES IN SCHEMA identity, social, dating, private, safety, chat FROM PUBLIC;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA identity, social, dating, private, safety, chat FROM PUBLIC;

-- API: DML only, no DDL (not owner), no TRUNCATE grant. Broad DML across schemas
-- is required by the monolith's cross-schema joins; per-domain app split is
-- deferred to service extraction (docs/READINESS.md gap 4).
GRANT USAGE ON SCHEMA identity, social, dating, private, safety, chat TO nexus_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA identity, social, dating, private, safety, chat TO nexus_app;
GRANT USAGE ON ALL SEQUENCES IN SCHEMA chat, safety, private TO nexus_app;

ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA identity GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA social GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA dating GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA private GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA safety GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA chat GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA chat GRANT USAGE ON SEQUENCES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA safety GRANT USAGE ON SEQUENCES TO nexus_app;
ALTER DEFAULT PRIVILEGES FOR ROLE nexus_owner IN SCHEMA private GRANT USAGE ON SEQUENCES TO nexus_app;

-- Moderator: status decisions on social content + audit trail. Nothing else.
GRANT USAGE ON SCHEMA social, safety TO nexus_moderator;
GRANT UPDATE ON social.posts, social.comments, social.events TO nexus_moderator;
GRANT INSERT ON safety.audit TO nexus_moderator;

-- Janitor: retention deletes only. Cannot read message bodies or exact fixes.
GRANT USAGE ON SCHEMA identity, private, chat TO nexus_janitor;
GRANT DELETE ON identity.otp, identity.sessions, private.outbox, chat.tombstones TO nexus_janitor;

-- Analytics: aggregate-visible data only; no identity, location, chat or safety access.
GRANT USAGE ON SCHEMA social, dating TO nexus_readonly;
GRANT SELECT ON social.posts, social.events TO nexus_readonly;
GRANT SELECT (profile_id, user_id, nickname, bio, photos, prompts, interests, gender, age_min, age_max, radius_km) ON dating.dating_profiles TO nexus_readonly;

UPDATE identity.schema_version SET version = 2;
COMMIT;
