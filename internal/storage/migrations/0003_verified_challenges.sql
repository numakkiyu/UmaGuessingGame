-- Low-request solo challenges have no room, stream, membership or outbox.
-- Immutable public vectors are stored once per pack; the selected answer stays here.
CREATE TABLE verified_content (
  id TEXT PRIMARY KEY,
  characters_json TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
CREATE TABLE verified_challenges (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  content_id TEXT NOT NULL REFERENCES verified_content(id),
  answer_id TEXT NOT NULL,
  wrong_budget INTEGER NOT NULL CHECK(wrong_budget BETWEEN 4 AND 12),
  revision INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL CHECK(status IN ('active','solved','failed','ended')),
  state_json TEXT NOT NULL,
  ticket TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_verified_session_created ON verified_challenges(session_id,created_at);
CREATE INDEX idx_verified_active ON verified_challenges(status,expires_at);
CREATE TABLE verified_receipts (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  request_key TEXT NOT NULL,
  challenge_id TEXT NOT NULL REFERENCES verified_challenges(id) ON DELETE CASCADE,
  payload_hash TEXT NOT NULL,
  response_json TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY(session_id,operation,request_key)
);
