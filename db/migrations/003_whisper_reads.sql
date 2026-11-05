BEGIN;
-- P0-2: per-recipient Whisper semantics. A read marks the message consumed for
-- that reader only; the shared row is physically deleted only after every
-- non-sender member of the room has read it. Prevents first-reader wipe of
-- group rooms (false-deletion trust damage).
CREATE TABLE chat.whisper_reads (
  message_id uuid NOT NULL REFERENCES chat.messages ON DELETE CASCADE,
  reader uuid NOT NULL REFERENCES identity.accounts ON DELETE CASCADE,
  read_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(message_id, reader));
CREATE INDEX ON chat.whisper_reads(reader);
ALTER TABLE chat.whisper_reads OWNER TO nexus_owner;
GRANT SELECT, INSERT, UPDATE, DELETE ON chat.whisper_reads TO nexus_app;
UPDATE identity.schema_version SET version = 3;
COMMIT;
