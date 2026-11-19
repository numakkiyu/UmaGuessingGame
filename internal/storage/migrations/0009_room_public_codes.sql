-- Six-digit room-local labels. They are never credentials.
ALTER TABLE memberships ADD COLUMN public_code TEXT CHECK(public_code IS NULL OR (length(public_code)=6 AND public_code NOT GLOB '*[^0-9]*'));
-- A per-room random offset and a permutation give existing rows collision-free
-- labels without changing their internal identifiers or old entry numbers.
CREATE TEMP TABLE room_code_offsets AS SELECT id, abs(random() % 1000000) AS offset FROM rooms;
WITH numbered AS (
 SELECT id, room_id, ROW_NUMBER() OVER (PARTITION BY room_id ORDER BY entry_number, id) AS n FROM memberships
)
UPDATE memberships SET public_code=(
 SELECT CASE WHEN n>1000000 THEN 'exhausted' ELSE printf('%06d',(n*4099+offset)%1000000) END
 FROM numbered JOIN room_code_offsets ON room_code_offsets.id=numbered.room_id WHERE numbered.id=memberships.id
);
DROP TABLE room_code_offsets;
CREATE UNIQUE INDEX idx_memberships_public_code ON memberships(room_id,public_code);
-- Random start with a bounded collision search. Never reuse departed labels.
CREATE TRIGGER memberships_assign_public_code AFTER INSERT ON memberships
WHEN NEW.public_code IS NULL
BEGIN
 UPDATE memberships SET public_code=(
  WITH RECURSIVE candidates(n,code) AS (
   SELECT 0,abs(random()%1000000)
   UNION ALL SELECT n+1,(code+1)%1000000 FROM candidates WHERE n<1023
  )
  SELECT printf('%06d',code) FROM candidates
  WHERE NOT EXISTS (SELECT 1 FROM memberships m WHERE m.room_id=NEW.room_id AND m.public_code=printf('%06d',code))
  ORDER BY n LIMIT 1
 ) WHERE id=NEW.id;
 SELECT CASE WHEN (SELECT public_code FROM memberships WHERE id=NEW.id) IS NULL
 THEN RAISE(ABORT,'room public code allocation exhausted') END;
END;
