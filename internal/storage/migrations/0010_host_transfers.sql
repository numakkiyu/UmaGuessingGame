-- Transfer commands survive an interrupted HTTP response and authority changes.
CREATE TABLE host_transfers (
 id TEXT PRIMARY KEY,
 room_id TEXT NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
 actor_session_id TEXT NOT NULL REFERENCES sessions(id),
 target_membership_id TEXT NOT NULL REFERENCES memberships(id),
 room_version INTEGER NOT NULL,
 creation_key TEXT,
 creation_hash TEXT,
 created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_host_transfer_command ON host_transfers(actor_session_id,creation_key) WHERE creation_key IS NOT NULL;
