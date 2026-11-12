-- Public room-local numbers are labels, never credentials. Existing memberships
-- receive deterministic numbers, including left members so rejoining is stable.
ALTER TABLE memberships ADD COLUMN entry_number INTEGER NOT NULL DEFAULT 0 CHECK(entry_number >= 0);
UPDATE memberships AS m SET entry_number = (
 SELECT COUNT(*) FROM memberships AS previous
 WHERE previous.room_id = m.room_id AND previous.rowid <= m.rowid
);
CREATE UNIQUE INDEX idx_memberships_entry_number ON memberships(room_id, entry_number);
CREATE TRIGGER memberships_assign_entry_number AFTER INSERT ON memberships
WHEN NEW.entry_number = 0
BEGIN
 UPDATE memberships SET entry_number = (
  SELECT COALESCE(MAX(entry_number),0) + 1 FROM memberships
  WHERE room_id=NEW.room_id AND id<>NEW.id
 ) WHERE id=NEW.id;
END;
