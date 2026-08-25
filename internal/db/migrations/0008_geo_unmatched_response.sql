ALTER TABLE site_geo_settings RENAME TO site_geo_settings_legacy;

CREATE TABLE site_geo_settings (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 0,
  default_action TEXT NOT NULL DEFAULT 'allow'
    CHECK (default_action IN ('allow', 'respond')),
  default_status_code INTEGER NOT NULL DEFAULT 403
    CHECK (default_status_code BETWEEN 400 AND 599),
  default_response_type TEXT NOT NULL DEFAULT 'text'
    CHECK (default_response_type IN ('html', 'text')),
  default_response_body TEXT NOT NULL DEFAULT '',
  desired_hash TEXT NOT NULL DEFAULT '',
  applied_hash TEXT NOT NULL DEFAULT '',
  apply_status TEXT NOT NULL DEFAULT 'disabled'
    CHECK (apply_status IN ('disabled', 'pending', 'applied', 'error')),
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO site_geo_settings (
  site_id, enabled, default_action, default_status_code, default_response_type,
  default_response_body, desired_hash, applied_hash, apply_status, last_error,
  created_at, updated_at
)
SELECT
  site_id,
  enabled,
  CASE default_action WHEN 'allow' THEN 'allow' ELSE 'respond' END,
  CASE default_action WHEN 'deny_444' THEN 444 ELSE 403 END,
  'text',
  '',
  desired_hash,
  applied_hash,
  apply_status,
  last_error,
  created_at,
  updated_at
FROM site_geo_settings_legacy;

DROP TABLE site_geo_settings_legacy;

CREATE INDEX idx_site_geo_settings_enabled
  ON site_geo_settings(enabled, site_id);
