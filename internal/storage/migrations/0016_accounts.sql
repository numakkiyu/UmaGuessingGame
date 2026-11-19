CREATE TABLE accounts (
    id TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL
);
-- Client ID scopes OAuth-only subjects and pairwise OIDC subjects to an application.
CREATE TABLE account_identities (
    issuer TEXT NOT NULL,
    client_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    created_at INTEGER NOT NULL,
    PRIMARY KEY(issuer,client_id,subject)
) WITHOUT ROWID;
CREATE TABLE account_sessions (
    id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    token_hash TEXT NOT NULL UNIQUE,
    previous_hash TEXT,
    previous_until INTEGER,
    created_at INTEGER NOT NULL,
    idle_expires_at INTEGER NOT NULL,
    absolute_expires_at INTEGER NOT NULL,
    renewed_at INTEGER NOT NULL,
    revoked_at INTEGER
);
CREATE INDEX account_sessions_expiry ON account_sessions(absolute_expires_at);
CREATE TABLE account_game_bindings (
    session_id TEXT PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
    account_id TEXT NOT NULL REFERENCES accounts(id),
    authorized_session_id TEXT REFERENCES account_sessions(id),
    created_at INTEGER NOT NULL
);
