BEGIN;
-- S2 P1 backlog: time-based Whisper expiry, notification mute preferences and
-- recent searches. The friend graph already has physical storage in
-- social.friendships; P1-3 only adds HTTP endpoints.

ALTER TABLE chat.messages ADD COLUMN whisper_expires_at timestamptz;
CREATE INDEX ON chat.messages(whisper_expires_at) WHERE mode='WHISPER';
UPDATE chat.messages SET whisper_expires_at=created_at+interval '24 hours' WHERE mode='WHISPER';

CREATE TABLE social.notification_prefs (
 user_id uuid PRIMARY KEY REFERENCES identity.accounts ON DELETE CASCADE,
 mute_activity boolean NOT NULL DEFAULT false,
 mute_follow boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT now());

CREATE TABLE social.recent_searches (
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 query text NOT NULL CHECK(length(query) BETWEEN 2 AND 100),
 kind text NOT NULL CHECK(kind IN ('people','posts','events')),
 searched_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,query,kind));
CREATE INDEX ON social.recent_searches(user_id,searched_at DESC);

ALTER TABLE social.notification_prefs OWNER TO nexus_owner;
ALTER TABLE social.recent_searches OWNER TO nexus_owner;

GRANT SELECT, INSERT, UPDATE ON social.notification_prefs TO nexus_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON social.recent_searches TO nexus_app;

-- Janitor sweep: the expiry predicates and tombstone metadata only. Message
-- ciphertext and normal bodies stay unreadable to this role.
GRANT SELECT (id, room_id, seq, mode, whisper_expires_at) ON chat.messages TO nexus_janitor;
GRANT DELETE ON chat.messages TO nexus_janitor;
GRANT INSERT ON chat.tombstones TO nexus_janitor;
GRANT SELECT (message_id) ON chat.tombstones TO nexus_janitor;
GRANT SELECT (created_at) ON identity.idempotency TO nexus_janitor;
GRANT DELETE ON identity.idempotency TO nexus_janitor;
GRANT USAGE ON SCHEMA social TO nexus_janitor;
GRANT SELECT (searched_at) ON social.recent_searches TO nexus_janitor;
GRANT DELETE ON social.recent_searches TO nexus_janitor;

UPDATE identity.schema_version SET version = 7;
COMMIT;
