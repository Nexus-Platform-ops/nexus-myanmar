BEGIN;
-- P2-1: the websocket replay stream reads private.outbox by (user_id,id) in id
-- order. The old poll filtered on created_at with no supporting index; replay
-- turns that into a range scan over a 24h-retention table.
CREATE INDEX ON private.outbox(user_id,id);

UPDATE identity.schema_version SET version = 8;
COMMIT;
