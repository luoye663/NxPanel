-- 0006_proxy_upstream_reference.sql - site proxy references to managed upstreams

ALTER TABLE site_proxy ADD COLUMN upstream_id TEXT
  REFERENCES nginx_upstreams(id) ON DELETE RESTRICT;
ALTER TABLE site_proxy ADD COLUMN upstream_scheme TEXT NOT NULL DEFAULT 'http'
  CHECK (upstream_scheme IN ('http', 'https'));
ALTER TABLE site_proxy ADD COLUMN proxy_ssl_server_name TEXT NOT NULL DEFAULT ''
  CHECK (length(CAST(proxy_ssl_server_name AS BLOB)) <= 253);
ALTER TABLE site_proxy ADD COLUMN proxy_ssl_verify INTEGER NOT NULL DEFAULT 0
  CHECK (proxy_ssl_verify IN (0, 1));
ALTER TABLE site_proxy ADD COLUMN proxy_ssl_trusted_certificate TEXT NOT NULL DEFAULT ''
  CHECK (length(CAST(proxy_ssl_trusted_certificate AS BLOB)) <= 4096);
ALTER TABLE site_proxy ADD COLUMN proxy_ssl_verify_depth INTEGER NOT NULL DEFAULT 0
  CHECK (proxy_ssl_verify_depth BETWEEN 0 AND 100);

CREATE INDEX idx_site_proxy_upstream_id ON site_proxy(upstream_id);
