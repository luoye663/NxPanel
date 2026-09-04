CREATE TABLE IF NOT EXISTS waf_site_policies (
    site_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK (enabled IN (0, 1)),
    mode TEXT NOT NULL CHECK (mode IN ('DetectionOnly', 'On')),
    paranoia_level INTEGER NOT NULL CHECK (paranoia_level BETWEEN 1 AND 4),
    inbound_threshold INTEGER NOT NULL,
    outbound_threshold INTEGER NOT NULL,
    response_status INTEGER NOT NULL,
    request_body_limit INTEGER NOT NULL,
    request_body_no_files_limit INTEGER NOT NULL,
    response_body_limit INTEGER NOT NULL,
    exclusions_json TEXT NOT NULL DEFAULT '[]',
    rules_version TEXT NOT NULL DEFAULT '4.25.0',
    updated_at TEXT NOT NULL,
    FOREIGN KEY (site_id) REFERENCES sites(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_waf_site_policies_enabled
    ON waf_site_policies(enabled, updated_at DESC);
