-- A group identifies a proposed match before its room exists.
ALTER TABLE matchmaking_tickets ADD COLUMN pool_id TEXT NOT NULL DEFAULT 'classic-duel';
ALTER TABLE matchmaking_tickets ADD COLUMN match_group TEXT;
ALTER TABLE matchmaking_tickets ADD COLUMN lease_until INTEGER NOT NULL DEFAULT 0;
ALTER TABLE matchmaking_tickets ADD COLUMN cooldown_until INTEGER NOT NULL DEFAULT 0;

-- Old pending pairs used a different confirmation contract. Release them once.
UPDATE matchmaking_tickets SET status='expired', matched_with=NULL
WHERE room_id IS NULL AND status IN ('queueing','matched-pending-accept','ready');
CREATE INDEX idx_matchmaking_pool ON matchmaking_tickets(pool_id,status,created_at);
CREATE INDEX idx_matchmaking_group ON matchmaking_tickets(match_group) WHERE match_group IS NOT NULL;

CREATE TABLE matchmaking_proofs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL UNIQUE REFERENCES sessions(id),
    pool_id TEXT NOT NULL,
    bits INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    used INTEGER NOT NULL DEFAULT 0 CHECK(used IN (0,1))
);
CREATE INDEX idx_matchmaking_proofs_expiry ON matchmaking_proofs(expires_at);
ALTER TABLE matchmaking_tickets ADD COLUMN creation_key TEXT;
ALTER TABLE matchmaking_tickets ADD COLUMN creation_hash TEXT;
CREATE UNIQUE INDEX idx_matchmaking_creation ON matchmaking_tickets(session_id,creation_key) WHERE creation_key IS NOT NULL;

-- Unstarted legacy match rooms need an explicit timed classic variant.
UPDATE rooms SET config_json=json_set(config_json,
    '$.modeConfig.variant',CASE WHEN capacity=2 THEN 'friend' ELSE 'public' END,
    '$.modeConfig.durationSeconds',180)
WHERE status IN ('lobby','ready') AND json_extract(config_json,'$.kind')='matchmaking'
  AND json_extract(config_json,'$.modeConfig.modeId')='classic'
  AND NOT EXISTS(SELECT 1 FROM rounds WHERE rounds.room_id=rooms.id);
