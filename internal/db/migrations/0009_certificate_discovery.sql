ALTER TABLE certificates ADD COLUMN source TEXT NOT NULL DEFAULT 'panel'
  CHECK (source IN ('panel', 'external'));

CREATE TABLE certificate_sites (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  certificate_id TEXT NOT NULL REFERENCES certificates(id) ON DELETE RESTRICT,
  config_path TEXT NOT NULL,
  file_hash TEXT NOT NULL,
  server_hash TEXT NOT NULL,
  cert_path TEXT NOT NULL,
  key_path TEXT NOT NULL
);
CREATE INDEX idx_certificate_sites_certificate ON certificate_sites(certificate_id);
