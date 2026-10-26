ALTER TABLE sites ADD COLUMN maintenance_enabled INTEGER NOT NULL DEFAULT 0 CHECK (maintenance_enabled IN (0,1));

CREATE TABLE maintenance_defaults (
  id INTEGER PRIMARY KEY CHECK (id=1),
  html TEXT,
  version INTEGER NOT NULL DEFAULT 0
);
INSERT INTO maintenance_defaults(id) VALUES(1);

CREATE TABLE site_maintenance (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  desired_enabled INTEGER NOT NULL DEFAULT 0 CHECK(desired_enabled IN(0,1)),
  html_override TEXT,
  version INTEGER NOT NULL DEFAULT 0,
  apply_status TEXT NOT NULL DEFAULT 'synced' CHECK(apply_status IN('synced','pending')),
  operation_id TEXT NOT NULL DEFAULT ''
);
