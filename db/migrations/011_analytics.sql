BEGIN;
-- P2-5: analytics isolation prep. Per-domain application roles cannot be split
-- while the monolith needs cross-schema joins (READINESS 4), but analytics work
-- can stop using nexus_readonly by reading a dedicated aggregate-only schema.
-- Views are security-definer (owned by nexus_owner): the analytics role needs no
-- grant on any base schema, and small township cells are suppressed below k=5.
CREATE ROLE nexus_analytics NOLOGIN;
CREATE SCHEMA analytics;

CREATE VIEW analytics.post_volume AS
 SELECT township, date_trunc('day',created_at) AS day, count(*)::bigint AS posts
 FROM social.posts WHERE status='LIVE' GROUP BY 1,2 HAVING count(*)>=5;

CREATE VIEW analytics.event_volume AS
 SELECT township, date_trunc('day',starts_at) AS day, count(*)::bigint AS events
 FROM social.events WHERE status='LIVE' GROUP BY 1,2 HAVING count(*)>=5;

CREATE VIEW analytics.message_volume AS
 SELECT date_trunc('day',created_at) AS day, mode, count(*)::bigint AS messages
 FROM chat.messages GROUP BY 1,2;

CREATE VIEW analytics.report_volume AS
 SELECT date_trunc('day',created_at) AS day, category, count(*)::bigint AS reports
 FROM safety.reports GROUP BY 1,2 HAVING count(*)>=5;

CREATE VIEW analytics.session_volume AS
 SELECT date_trunc('day',created_at) AS day, count(*)::bigint AS sessions
 FROM identity.sessions GROUP BY 1;

ALTER SCHEMA analytics OWNER TO nexus_owner;
ALTER VIEW analytics.post_volume OWNER TO nexus_owner;
ALTER VIEW analytics.event_volume OWNER TO nexus_owner;
ALTER VIEW analytics.message_volume OWNER TO nexus_owner;
ALTER VIEW analytics.report_volume OWNER TO nexus_owner;
ALTER VIEW analytics.session_volume OWNER TO nexus_owner;

REVOKE ALL ON SCHEMA analytics FROM PUBLIC;
GRANT USAGE ON SCHEMA analytics TO nexus_analytics;
GRANT SELECT ON ALL TABLES IN SCHEMA analytics TO nexus_analytics;

UPDATE identity.schema_version SET version = 11;
COMMIT;
