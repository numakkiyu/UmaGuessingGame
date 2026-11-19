-- Matchmaking pairing.
--
-- Why this migration exists: the original ticket table had no way to record who a
-- ticket was paired with, so the queue could only ever hold lone waiters. Pairing
-- is now decided synchronously inside the ticket-creation transaction (see
-- room.findWaitingPartner), which needs a partner pointer on the row.
--
-- The status CHECK is also widened. The Go layer and the contract use
-- queueing / matched-pending-accept / ready / playing / finished, while the
-- original constraint listed queued / matched / confirmed. That mismatch meant the
-- documented states could not be written at all. SQLite cannot alter a CHECK in
-- place, so the table is rebuilt.
--
-- Rebuild steps follow the documented SQLite procedure: create the replacement,
-- copy, drop, rename. No data is lost — existing rows map onto the new vocabulary.

CREATE TABLE IF NOT EXISTS matchmaking_tickets_new (
    id           TEXT PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES sessions (id),
    mode_id      TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN (
                     'queueing', 'matched-pending-accept', 'ready',
                     'playing', 'cancelled', 'expired', 'finished')),
    room_id      TEXT REFERENCES rooms (id),
    -- matched_with points at the partner ticket while a pairing is pending. It is
    -- cleared once both sides are ready, so it only ever names an unaccepted pair.
    matched_with TEXT,
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

INSERT INTO matchmaking_tickets_new (id, session_id, mode_id, status, room_id, created_at, expires_at, updated_at)
SELECT id, session_id, mode_id,
       CASE status
           WHEN 'queued'    THEN 'queueing'
           WHEN 'matched'   THEN 'matched-pending-accept'
           WHEN 'confirmed' THEN 'ready'
           ELSE status
       END,
       room_id, created_at, expires_at, updated_at
FROM matchmaking_tickets;

DROP TABLE matchmaking_tickets;
ALTER TABLE matchmaking_tickets_new RENAME TO matchmaking_tickets;

-- A session holds at most one active ticket.
CREATE UNIQUE INDEX IF NOT EXISTS idx_matchmaking_active
    ON matchmaking_tickets (session_id)
    WHERE status IN ('queueing', 'matched-pending-accept', 'ready', 'playing');
