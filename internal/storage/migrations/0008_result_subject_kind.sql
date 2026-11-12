ALTER TABLE round_results ADD COLUMN subject_kind TEXT NOT NULL DEFAULT 'player'
    CHECK (subject_kind IN ('player', 'team'));

-- A team id belongs to the round's room. Historical individual scores retain
-- their default type; no score or correction value is rewritten.
UPDATE round_results SET subject_kind = 'team'
WHERE EXISTS (
    SELECT 1 FROM teams t JOIN rounds r ON r.room_id = t.room_id
    WHERE r.id = round_results.round_id AND t.id = round_results.subject_id
);
