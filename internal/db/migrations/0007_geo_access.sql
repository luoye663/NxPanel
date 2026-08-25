CREATE TABLE IF NOT EXISTS geoip_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  account_id TEXT NOT NULL DEFAULT '',
  license_key_encrypted TEXT NOT NULL DEFAULT '',
  auto_update INTEGER NOT NULL DEFAULT 1,
  trusted_proxies_json TEXT NOT NULL DEFAULT '[]',
  active_db_path TEXT NOT NULL DEFAULT '',
  active_cache_path TEXT NOT NULL DEFAULT '',
  checksum TEXT NOT NULL DEFAULT '',
  build_epoch INTEGER NOT NULL DEFAULT 0,
  countries_json TEXT NOT NULL DEFAULT '[]',
  last_attempt_at TEXT,
  last_success_at TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO geoip_settings (id) VALUES (1);

CREATE TABLE IF NOT EXISTS site_geo_settings (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 0,
  default_action TEXT NOT NULL DEFAULT 'allow'
    CHECK (default_action IN ('allow', 'deny_403', 'deny_444')),
  desired_hash TEXT NOT NULL DEFAULT '',
  applied_hash TEXT NOT NULL DEFAULT '',
  apply_status TEXT NOT NULL DEFAULT 'disabled'
    CHECK (apply_status IN ('disabled', 'pending', 'applied', 'error')),
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS site_geo_rules (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  countries_json TEXT NOT NULL DEFAULT '[]',
  action TEXT NOT NULL CHECK (action IN ('allow', 'deny_403', 'deny_444')),
  enabled INTEGER NOT NULL DEFAULT 0,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_site_geo_rules_site_order
  ON site_geo_rules(site_id, sort_order, created_at);
CREATE INDEX IF NOT EXISTS idx_site_geo_settings_enabled
  ON site_geo_settings(enabled, site_id);
