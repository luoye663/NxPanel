CREATE TABLE IF NOT EXISTS plugin_installations (
    plugin_id TEXT PRIMARY KEY,
    active_version TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    health_status TEXT NOT NULL DEFAULT 'unknown',
    last_error TEXT NOT NULL DEFAULT '',
    installed_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS plugin_versions (
    plugin_id TEXT NOT NULL,
    version TEXT NOT NULL,
    package_sha256 TEXT NOT NULL,
    manifest_json TEXT NOT NULL,
    install_path TEXT NOT NULL,
    installed_at TEXT NOT NULL,
    PRIMARY KEY (plugin_id, version),
    FOREIGN KEY (plugin_id) REFERENCES plugin_installations(plugin_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS plugin_permissions (
    plugin_id TEXT NOT NULL,
    permission TEXT NOT NULL,
    approved_at TEXT NOT NULL,
    PRIMARY KEY (plugin_id, permission),
    FOREIGN KEY (plugin_id) REFERENCES plugin_installations(plugin_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS plugin_kv (
    plugin_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value BLOB NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (plugin_id, key)
);

CREATE TABLE IF NOT EXISTS plugin_events (
    id TEXT PRIMARY KEY,
    plugin_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_plugin_events_plugin_created
    ON plugin_events(plugin_id, created_at DESC);
