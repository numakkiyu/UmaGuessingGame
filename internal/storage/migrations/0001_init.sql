-- UmaGuessingGame v2 runtime schema.
-- Initial relational schema for rooms, sessions, rounds and results.
-- All business time columns are UTC milliseconds since epoch (INTEGER).
-- All identifiers are opaque TEXT; display names are never foreign keys.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version     TEXT PRIMARY KEY,
    checksum    TEXT NOT NULL,
    applied_at  INTEGER NOT NULL
) WITHOUT ROWID;

-- Anonymous sessions. The cookie carries an opaque token; only its hash is stored.
CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY,
    token_hash      TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    csrf_token_hash TEXT NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('player', 'display')),
    created_at      INTEGER NOT NULL,
    expires_at      INTEGER NOT NULL,
    revoked_at      INTEGER
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions (expires_at);

CREATE TABLE IF NOT EXISTS rooms (
    id               TEXT PRIMARY KEY,
    invite_code      TEXT,
    owner_session_id TEXT NOT NULL REFERENCES sessions (id),
    name             TEXT NOT NULL,
    status           TEXT NOT NULL CHECK (status IN ('lobby', 'ready', 'playing', 'finished', 'archived', 'cancelled')),
    room_version     INTEGER NOT NULL DEFAULT 1,
    config_json      TEXT NOT NULL DEFAULT '{}',
    capacity         INTEGER NOT NULL DEFAULT 100 CHECK (capacity > 0 AND capacity <= 500),
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);
-- A room's invite code must be unique among rooms that are still joinable.
CREATE UNIQUE INDEX IF NOT EXISTS idx_rooms_active_invite
    ON rooms (invite_code)
    WHERE invite_code IS NOT NULL AND status NOT IN ('archived', 'cancelled');

CREATE TABLE IF NOT EXISTS memberships (
    id           TEXT PRIMARY KEY,
    room_id      TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    session_id   TEXT NOT NULL REFERENCES sessions (id),
    role         TEXT NOT NULL CHECK (role IN ('player', 'team-captain', 'spectator', 'host', 'display')),
    team_id      TEXT,
    ready        INTEGER NOT NULL DEFAULT 0 CHECK (ready IN (0, 1)),
    joined_at    INTEGER NOT NULL,
    left_at      INTEGER,
    revoked_at   INTEGER
);
-- A session participates at most once per room.
CREATE UNIQUE INDEX IF NOT EXISTS idx_memberships_room_session ON memberships (room_id, session_id);
CREATE INDEX IF NOT EXISTS idx_memberships_room ON memberships (room_id);
CREATE INDEX IF NOT EXISTS idx_memberships_session ON memberships (session_id);

CREATE TABLE IF NOT EXISTS teams (
    id          TEXT PRIMARY KEY,
    room_id     TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    label       TEXT NOT NULL,
    color       TEXT,
    created_at  INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_teams_room_label ON teams (room_id, label);

-- Management authority is orthogonal to participation role.
CREATE TABLE IF NOT EXISTS room_grants (
    id          TEXT PRIMARY KEY,
    room_id     TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    session_id  TEXT NOT NULL REFERENCES sessions (id),
    capability  TEXT NOT NULL CHECK (capability IN ('manage-room', 'host')),
    granted_at  INTEGER NOT NULL,
    revoked_at  INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_room_grants_unique
    ON room_grants (room_id, session_id, capability)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS control_leases (
    id            TEXT PRIMARY KEY,
    room_id       TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    round_id      TEXT,
    slot          TEXT NOT NULL,
    membership_id TEXT NOT NULL REFERENCES memberships (id),
    generation    INTEGER NOT NULL DEFAULT 1,
    issued_at     INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,
    revoked_at    INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_control_leases_active
    ON control_leases (room_id, slot)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS rounds (
    id                  TEXT PRIMARY KEY,
    room_id             TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    ordinal             INTEGER NOT NULL,
    mode_id             TEXT NOT NULL,
    rules_version       TEXT NOT NULL,
    dataset_version     TEXT NOT NULL,
    content_pack_version TEXT NOT NULL,
    status              TEXT NOT NULL CHECK (status IN ('prepared', 'briefing', 'collecting', 'locked', 'revealed', 'settled', 'cancelled')),
    phase_version       INTEGER NOT NULL DEFAULT 1,
    deadline_at         INTEGER,
    paused_at           INTEGER,
    paused_remaining_ms INTEGER,
    paused_previous_phase TEXT,
    seed                TEXT NOT NULL,
    config_json         TEXT NOT NULL DEFAULT '{}',
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_rounds_room_ordinal ON rounds (room_id, ordinal);
-- At most one live round per room.
CREATE UNIQUE INDEX IF NOT EXISTS idx_rounds_room_current
    ON rounds (room_id)
    WHERE status NOT IN ('settled', 'cancelled');

-- Server-only secrets. Never serialized into any client projection.
CREATE TABLE IF NOT EXISTS round_private_state (
    round_id    TEXT NOT NULL REFERENCES rounds (id) ON DELETE CASCADE,
    scope       TEXT NOT NULL,
    state_json  TEXT NOT NULL,
    PRIMARY KEY (round_id, scope)
);

CREATE TABLE IF NOT EXISTS participant_round_state (
    round_id        TEXT NOT NULL REFERENCES rounds (id) ON DELETE CASCADE,
    subject_id      TEXT NOT NULL,
    subject_kind    TEXT NOT NULL CHECK (subject_kind IN ('player', 'team')),
    state_json      TEXT NOT NULL,
    updated_at      INTEGER NOT NULL,
    PRIMARY KEY (round_id, subject_id)
);

CREATE TABLE IF NOT EXISTS submissions (
    id                  TEXT PRIMARY KEY,
    round_id            TEXT NOT NULL REFERENCES rounds (id) ON DELETE CASCADE,
    subject_id          TEXT NOT NULL,
    subject_kind        TEXT NOT NULL CHECK (subject_kind IN ('player', 'team')),
    slot                TEXT NOT NULL,
    revision            INTEGER NOT NULL,
    action_kind         TEXT NOT NULL,
    payload_json        TEXT NOT NULL,
    received_at         INTEGER NOT NULL,
    accepted            INTEGER NOT NULL CHECK (accepted IN (0, 1)),
    outcome_code        TEXT,
    created_at          INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_submissions_unique
    ON submissions (round_id, subject_id, slot, revision);
CREATE INDEX IF NOT EXISTS idx_submissions_round ON submissions (round_id);

CREATE TABLE IF NOT EXISTS submission_heads (
    round_id      TEXT NOT NULL REFERENCES rounds (id) ON DELETE CASCADE,
    subject_id    TEXT NOT NULL,
    slot          TEXT NOT NULL,
    revision      INTEGER NOT NULL,
    submission_id TEXT NOT NULL REFERENCES submissions (id),
    locked_at     INTEGER,
    PRIMARY KEY (round_id, subject_id, slot)
);

-- Classic guesses: attempt ordinal is unique per participant per round.
CREATE TABLE IF NOT EXISTS classic_guesses (
    round_id      TEXT NOT NULL REFERENCES rounds (id) ON DELETE CASCADE,
    subject_id    TEXT NOT NULL,
    attempt       INTEGER NOT NULL,
    character_id  TEXT NOT NULL,
    correct       INTEGER NOT NULL CHECK (correct IN (0, 1)),
    feedback_json TEXT NOT NULL,
    received_at   INTEGER NOT NULL,
    PRIMARY KEY (round_id, subject_id, attempt)
);
-- Re-guessing an already-submitted character in the same round is rejected.
CREATE UNIQUE INDEX IF NOT EXISTS idx_classic_guesses_character
    ON classic_guesses (round_id, subject_id, character_id);

CREATE TABLE IF NOT EXISTS event_sessions (
    id            TEXT PRIMARY KEY,
    room_id       TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    host_session_id TEXT NOT NULL REFERENCES sessions (id),
    name          TEXT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('draft', 'lobby', 'running', 'completed', 'cancelled')),
    score_budget  INTEGER NOT NULL DEFAULT 100,
    event_version INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);
-- One active programme per room.
CREATE UNIQUE INDEX IF NOT EXISTS idx_event_sessions_room_active
    ON event_sessions (room_id)
    WHERE status NOT IN ('completed', 'cancelled');

CREATE TABLE IF NOT EXISTS event_segments (
    id            TEXT PRIMARY KEY,
    event_id      TEXT NOT NULL REFERENCES event_sessions (id) ON DELETE CASCADE,
    ordinal       INTEGER NOT NULL,
    mode_id       TEXT NOT NULL,
    name          TEXT NOT NULL,
    status        TEXT NOT NULL CHECK (status IN ('planned', 'running', 'completed', 'voided')),
    budget        INTEGER NOT NULL DEFAULT 100,
    round_id      TEXT REFERENCES rounds (id),
    config_json   TEXT NOT NULL DEFAULT '{}'
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_event_segments_ordinal ON event_segments (event_id, ordinal);

CREATE TABLE IF NOT EXISTS round_results (
    round_id        TEXT NOT NULL REFERENCES rounds (id) ON DELETE CASCADE,
    subject_id      TEXT NOT NULL,
    score_units     INTEGER NOT NULL,
    max_score_units INTEGER NOT NULL,
    explanation_json TEXT NOT NULL,
    settled_version INTEGER NOT NULL,
    PRIMARY KEY (round_id, subject_id)
);

CREATE TABLE IF NOT EXISTS segment_results (
    segment_id      TEXT NOT NULL REFERENCES event_segments (id) ON DELETE CASCADE,
    subject_id      TEXT NOT NULL,
    score_units     INTEGER NOT NULL,
    max_score_units INTEGER NOT NULL,
    PRIMARY KEY (segment_id, subject_id)
);

CREATE TABLE IF NOT EXISTS score_corrections (
    id            TEXT PRIMARY KEY,
    event_id      TEXT NOT NULL REFERENCES event_sessions (id) ON DELETE CASCADE,
    segment_id    TEXT REFERENCES event_segments (id),
    subject_id    TEXT NOT NULL,
    reason        TEXT NOT NULL,
    before_units  INTEGER NOT NULL,
    after_units   INTEGER NOT NULL,
    corrected_by  TEXT NOT NULL REFERENCES sessions (id),
    created_at    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS idempotency_receipts (
    session_id    TEXT NOT NULL REFERENCES sessions (id),
    route_scope   TEXT NOT NULL,
    idem_key      TEXT NOT NULL,
    payload_hash  TEXT NOT NULL,
    status_code   INTEGER NOT NULL,
    body_json     TEXT NOT NULL,
    created_at    INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,
    PRIMARY KEY (session_id, route_scope, idem_key)
);
CREATE INDEX IF NOT EXISTS idx_idem_expires ON idempotency_receipts (expires_at);

CREATE TABLE IF NOT EXISTS outbox_events (
    id             TEXT PRIMARY KEY,
    room_id        TEXT NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    event_sequence INTEGER NOT NULL,
    type           TEXT NOT NULL,
    payload_json   TEXT NOT NULL,
    created_at     INTEGER NOT NULL,
    dispatched_at  INTEGER
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_room_seq ON outbox_events (room_id, event_sequence);
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox_events (created_at) WHERE dispatched_at IS NULL;

CREATE TABLE IF NOT EXISTS matchmaking_tickets (
    id             TEXT PRIMARY KEY,
    session_id     TEXT NOT NULL REFERENCES sessions (id),
    mode_id        TEXT NOT NULL,
    status         TEXT NOT NULL CHECK (status IN ('queued', 'matched', 'confirmed', 'cancelled', 'expired')),
    room_id        TEXT REFERENCES rooms (id),
    created_at     INTEGER NOT NULL,
    expires_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
-- A session holds at most one active ticket.
CREATE UNIQUE INDEX IF NOT EXISTS idx_matchmaking_active
    ON matchmaking_tickets (session_id)
    WHERE status IN ('queued', 'matched');

CREATE TABLE IF NOT EXISTS audit_events (
    id          TEXT PRIMARY KEY,
    room_id     TEXT REFERENCES rooms (id) ON DELETE CASCADE,
    event_id    TEXT REFERENCES event_sessions (id) ON DELETE CASCADE,
    actor_id    TEXT NOT NULL,
    action      TEXT NOT NULL,
    detail_json TEXT NOT NULL DEFAULT '{}',
    created_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_room ON audit_events (room_id, created_at);

CREATE TABLE IF NOT EXISTS asset_cache_entries (
    asset_id        TEXT NOT NULL,
    content_version TEXT NOT NULL,
    local_path      TEXT,
    sha256          TEXT,
    status          TEXT NOT NULL CHECK (status IN ('missing', 'cached', 'stale', 'failed')),
    checked_at      INTEGER NOT NULL,
    PRIMARY KEY (asset_id, content_version)
);

CREATE TABLE IF NOT EXISTS recovery_grants (
    id            TEXT PRIMARY KEY,
    membership_id TEXT NOT NULL REFERENCES memberships (id) ON DELETE CASCADE,
    code_hash     TEXT NOT NULL UNIQUE,
    created_by    TEXT NOT NULL REFERENCES sessions (id),
    created_at    INTEGER NOT NULL,
    expires_at    INTEGER NOT NULL,
    consumed_at   INTEGER
);

CREATE TABLE IF NOT EXISTS display_grants (
    id         TEXT PRIMARY KEY,
    event_id   TEXT NOT NULL REFERENCES event_sessions (id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL UNIQUE,
    created_by TEXT NOT NULL REFERENCES sessions (id),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    consumed_at INTEGER
);
