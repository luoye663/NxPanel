CREATE TABLE site_access_policies (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  mode TEXT NOT NULL DEFAULT 'legacy' CHECK (mode IN ('legacy', 'unified')),
  version INTEGER NOT NULL CHECK (version > 0),
  desired_json TEXT NOT NULL,
  applied_json TEXT NOT NULL DEFAULT '',
  applied_version INTEGER NOT NULL DEFAULT 0,
  apply_status TEXT NOT NULL DEFAULT 'pending' CHECK (apply_status IN ('pending', 'applied', 'error')),
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
