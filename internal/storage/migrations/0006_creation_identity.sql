-- Resource identity and request hash commit with the business row. A lost HTTP
-- receipt or a process exit between commit and response cannot create duplicates.
ALTER TABLE rooms ADD COLUMN creation_key TEXT;
ALTER TABLE rooms ADD COLUMN creation_hash TEXT;
CREATE UNIQUE INDEX idx_rooms_creation_key ON rooms(owner_session_id,creation_key) WHERE creation_key IS NOT NULL;
ALTER TABLE event_sessions ADD COLUMN creation_key TEXT;
ALTER TABLE event_sessions ADD COLUMN creation_hash TEXT;
CREATE UNIQUE INDEX idx_events_creation_key ON event_sessions(host_session_id,creation_key) WHERE creation_key IS NOT NULL;
