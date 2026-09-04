DROP TABLE IF EXISTS plugin_license;

CREATE TABLE IF NOT EXISTS plugin_authorizations (
    authorization_id TEXT PRIMARY KEY,
    account_id TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    email_masked TEXT NOT NULL DEFAULT '',
    access_ciphertext BLOB NOT NULL,
    access_nonce BLOB NOT NULL,
    refresh_ciphertext BLOB NOT NULL,
    refresh_nonce BLOB NOT NULL,
    access_expires_at TEXT NOT NULL,
    refresh_expires_at TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reauthorization_required')),
    created_at TEXT NOT NULL,
    last_used_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_plugin_authorizations_account
    ON plugin_authorizations(account_id);

CREATE TABLE IF NOT EXISTS plugin_authorization_bindings (
    plugin_id TEXT PRIMARY KEY,
    authorization_id TEXT NOT NULL REFERENCES plugin_authorizations(authorization_id) ON DELETE CASCADE,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS plugin_instance_identity (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    instance_uuid TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL
);
