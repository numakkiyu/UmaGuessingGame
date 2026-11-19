-- Ownership may move, but the identity of an original creation command may not.
ALTER TABLE rooms ADD COLUMN creator_session_id TEXT REFERENCES sessions(id);
ALTER TABLE event_sessions ADD COLUMN creator_session_id TEXT REFERENCES sessions(id);
UPDATE rooms SET creator_session_id=owner_session_id;
UPDATE event_sessions SET creator_session_id=host_session_id;
DROP INDEX idx_rooms_creation_key;
DROP INDEX idx_events_creation_key;
CREATE UNIQUE INDEX idx_rooms_creation_key ON rooms(creator_session_id,creation_key) WHERE creation_key IS NOT NULL;
CREATE UNIQUE INDEX idx_events_creation_key ON event_sessions(creator_session_id,creation_key) WHERE creation_key IS NOT NULL;
CREATE TRIGGER rooms_set_creator AFTER INSERT ON rooms WHEN NEW.creator_session_id IS NULL
BEGIN UPDATE rooms SET creator_session_id=NEW.owner_session_id WHERE id=NEW.id; END;
CREATE TRIGGER events_set_creator AFTER INSERT ON event_sessions WHEN NEW.creator_session_id IS NULL
BEGIN UPDATE event_sessions SET creator_session_id=NEW.host_session_id WHERE id=NEW.id; END;
