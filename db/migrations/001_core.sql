BEGIN;
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE SCHEMA IF NOT EXISTS identity;
CREATE SCHEMA IF NOT EXISTS social;
CREATE SCHEMA IF NOT EXISTS dating;
CREATE SCHEMA IF NOT EXISTS private;
CREATE SCHEMA IF NOT EXISTS safety;
CREATE SCHEMA IF NOT EXISTS chat;
CREATE TABLE identity.accounts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), phone_hash text NOT NULL UNIQUE,
 phone_cipher bytea NOT NULL, dob date NOT NULL, phone_verified boolean NOT NULL DEFAULT false,
 tier smallint NOT NULL DEFAULT 0 CHECK(tier BETWEEN 0 AND 3),
 spoof_risk real NOT NULL DEFAULT 0 CHECK(spoof_risk BETWEEN 0 AND 1),
 restricted boolean NOT NULL DEFAULT false, ghost boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE identity.otp (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), phone_hash text NOT NULL,
 phone_cipher bytea NOT NULL, code_hash text NOT NULL, attempts int NOT NULL DEFAULT 0,
 expires_at timestamptz NOT NULL, consumed boolean NOT NULL DEFAULT false);
CREATE INDEX ON identity.otp(expires_at);
CREATE TABLE identity.sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
 refresh_hash text NOT NULL UNIQUE, device_label text NOT NULL,
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE social.user_profiles (
 user_id uuid PRIMARY KEY REFERENCES identity.accounts ON DELETE CASCADE,
 real_name text NOT NULL CHECK(length(real_name) BETWEEN 1 AND 80),
 handle text UNIQUE, bio text NOT NULL DEFAULT '', township text NOT NULL,
 avatar text NOT NULL DEFAULT '');
CREATE TABLE social.friendships (
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 friend_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 PRIMARY KEY(user_id,friend_id), CHECK(user_id <> friend_id));
CREATE TABLE dating.dating_profiles (
 profile_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 user_id uuid NOT NULL UNIQUE REFERENCES identity.accounts ON DELETE CASCADE,
 nickname text NOT NULL CHECK(length(nickname) BETWEEN 1 AND 40),
 bio text NOT NULL DEFAULT '', photos text[] NOT NULL DEFAULT '{}',
 prompts text[] NOT NULL DEFAULT '{}', interests text[] NOT NULL DEFAULT '{}',
 gender text NOT NULL DEFAULT 'unspecified', preferences text[] NOT NULL DEFAULT '{}',
 age_min int NOT NULL DEFAULT 18 CHECK(age_min>=18), age_max int NOT NULL DEFAULT 99,
 hide_from_social_friends boolean NOT NULL DEFAULT true,
 radius_km int NOT NULL DEFAULT 5 CHECK(radius_km IN (1,3,5,10)),
 radar_until timestamptz, CHECK(cardinality(photos)<=6), CHECK(age_max>=age_min));
CREATE TABLE private.user_locations (
 user_id uuid PRIMARY KEY REFERENCES identity.accounts ON DELETE CASCADE,
 fix_cipher bytea NOT NULL, coarse_geog geography(Point,4326) NOT NULL,
 township text NOT NULL, accuracy_m int NOT NULL CHECK(accuracy_m BETWEEN 0 AND 100),
 updated_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX ON private.user_locations USING gist(coarse_geog);
CREATE TABLE safety.blocks (
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 target_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 PRIMARY KEY(user_id,target_id), CHECK(user_id <> target_id));
CREATE TABLE safety.reports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), reporter uuid REFERENCES identity.accounts ON DELETE SET NULL,
 target_id uuid REFERENCES identity.accounts ON DELETE SET NULL, category text NOT NULL,
 severity text NOT NULL CHECK(severity IN ('MEDIUM','HIGH')), status text NOT NULL DEFAULT 'OPEN',
 details text NOT NULL, due_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE safety.audit (
 id bigserial PRIMARY KEY, actor uuid, action text NOT NULL, resource_id uuid,
 created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE social.posts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), author uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
 body text NOT NULL CHECK(length(body) BETWEEN 1 AND 5000), township text NOT NULL,
 audience text NOT NULL CHECK(audience IN ('PUBLIC','COMMUNITY','FRIENDS')),
 status text NOT NULL DEFAULT 'UNDER_REVIEW' CHECK(status IN ('UNDER_REVIEW','LIVE','REMOVED')),
 created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX ON social.posts(created_at DESC,id DESC) WHERE status='LIVE';
CREATE TABLE social.reactions (
 post_id uuid REFERENCES social.posts ON DELETE CASCADE,
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 type text NOT NULL CHECK(type IN ('LIKE','LOVE','HAHA','WOW','SAD','ANGRY')),
 PRIMARY KEY(post_id,user_id));
CREATE TABLE social.comments (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), post_id uuid NOT NULL REFERENCES social.posts ON DELETE CASCADE,
 author uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
 parent_id uuid REFERENCES social.comments ON DELETE CASCADE,
 body text NOT NULL CHECK(length(body) BETWEEN 1 AND 2000),
 status text NOT NULL DEFAULT 'UNDER_REVIEW' CHECK(status IN ('UNDER_REVIEW','LIVE','REMOVED')),
 created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE social.bookmarks (
 post_id uuid REFERENCES social.posts ON DELETE CASCADE,
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE, PRIMARY KEY(post_id,user_id));
CREATE TABLE social.shares (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), post_id uuid REFERENCES social.posts ON DELETE CASCADE,
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 mode text NOT NULL CHECK(mode IN ('RESHARE','SEND_DM','COPY_LINK')), created_at timestamptz DEFAULT now());
CREATE TABLE social.events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), host uuid REFERENCES identity.accounts ON DELETE CASCADE,
 title text NOT NULL, description text NOT NULL, township text NOT NULL,
 venue_area text NOT NULL, starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'UNDER_REVIEW' CHECK(status IN ('UNDER_REVIEW','LIVE','REMOVED')),
 CHECK(ends_at>starts_at));
CREATE TABLE social.rsvps (
 event_id uuid REFERENCES social.events ON DELETE CASCADE,
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 status text NOT NULL CHECK(status IN ('ATTEND','NOT_GOING')), PRIMARY KEY(event_id,user_id));
CREATE TABLE social.groups (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), owner uuid REFERENCES identity.accounts ON DELETE CASCADE,
 name text NOT NULL, township text NOT NULL, description text NOT NULL);
CREATE TABLE social.memberships (
 group_id uuid REFERENCES social.groups ON DELETE CASCADE,
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE, PRIMARY KEY(group_id,user_id));
CREATE TABLE dating.swipes (
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 target_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 intent text NOT NULL CHECK(intent IN ('LIKE','PASS')), PRIMARY KEY(user_id,target_id));
CREATE TABLE chat.rooms (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), kind text NOT NULL CHECK(kind IN ('MATCH','SOCIAL')),
 pair_key text UNIQUE, created_at timestamptz DEFAULT now());
CREATE TABLE chat.members (
 room_id uuid REFERENCES chat.rooms ON DELETE CASCADE,
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE, PRIMARY KEY(room_id,user_id));
CREATE TABLE chat.messages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), room_id uuid NOT NULL REFERENCES chat.rooms ON DELETE CASCADE,
 sender uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
 seq bigserial UNIQUE, body text NOT NULL CHECK(length(body) BETWEEN 1 AND 4000),
 mode text NOT NULL CHECK(mode IN ('NORMAL','WHISPER')),
 client_id uuid NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(room_id,sender,client_id));
CREATE INDEX ON chat.messages(room_id,seq);
CREATE TABLE chat.dedup (
 room_id uuid REFERENCES chat.rooms ON DELETE CASCADE,
 sender uuid REFERENCES identity.accounts ON DELETE CASCADE,
 client_id uuid NOT NULL, message_id uuid NOT NULL, seq bigint NOT NULL,
 PRIMARY KEY(room_id,sender,client_id));
CREATE TABLE chat.tombstones (
 message_id uuid PRIMARY KEY, room_id uuid REFERENCES chat.rooms ON DELETE CASCADE,
 seq bigint NOT NULL, expired_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE social.notifications (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES identity.accounts ON DELETE CASCADE,
 category text NOT NULL CHECK(category IN ('ACTIVITY','SAFETY','FOLLOW')),
 kind text NOT NULL, resource_id uuid, read_at timestamptz, created_at timestamptz DEFAULT now());
CREATE TABLE private.outbox (
 id bigserial PRIMARY KEY, topic text NOT NULL, user_id uuid,
 payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), delivered_at timestamptz);
CREATE TABLE identity.idempotency (
 user_id uuid REFERENCES identity.accounts ON DELETE CASCADE, key uuid,
 method_path text NOT NULL, body_hash text NOT NULL, status int NOT NULL, response jsonb NOT NULL,
 created_at timestamptz DEFAULT now(), PRIMARY KEY(user_id,key));
CREATE TABLE identity.schema_version (version int PRIMARY KEY);
INSERT INTO identity.schema_version VALUES (1);
COMMIT;
