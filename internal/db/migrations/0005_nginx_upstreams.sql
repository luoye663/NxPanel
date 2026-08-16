-- 0005_nginx_upstreams.sql - global Nginx upstream definitions

CREATE TABLE nginx_upstreams (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL COLLATE NOCASE UNIQUE
    CHECK (name = trim(name) AND length(name) BETWEEN 1 AND 63),
  algorithm TEXT NOT NULL CHECK (algorithm IN ('round_robin','least_conn','ip_hash','hash')),
  hash_key TEXT NOT NULL DEFAULT '',
  consistent INTEGER NOT NULL DEFAULT 0 CHECK (consistent IN (0, 1)),
  keepalive INTEGER NOT NULL DEFAULT 0 CHECK (keepalive BETWEEN 0 AND 10000),
  keepalive_requests INTEGER NOT NULL DEFAULT 0 CHECK (keepalive_requests BETWEEN 0 AND 1000000),
  keepalive_timeout_seconds INTEGER NOT NULL DEFAULT 0 CHECK (keepalive_timeout_seconds BETWEEN 0 AND 3600),
  advanced_directives TEXT NOT NULL DEFAULT '' CHECK (length(CAST(advanced_directives AS BLOB)) <= 16384),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK (
    (algorithm = 'hash' AND length(trim(hash_key)) > 0) OR
    (algorithm <> 'hash' AND hash_key = '' AND consistent = 0)
  ),
  CHECK (keepalive > 0 OR (keepalive_requests = 0 AND keepalive_timeout_seconds = 0))
);

CREATE TABLE nginx_upstream_servers (
  id TEXT PRIMARY KEY,
  upstream_id TEXT NOT NULL REFERENCES nginx_upstreams(id) ON DELETE CASCADE,
  address TEXT NOT NULL CHECK (address = trim(address) AND length(address) > 0),
  weight INTEGER NOT NULL DEFAULT 1 CHECK (weight BETWEEN 1 AND 256),
  max_fails INTEGER NOT NULL DEFAULT 1 CHECK (max_fails BETWEEN 0 AND 100),
  fail_timeout_seconds INTEGER NOT NULL DEFAULT 10 CHECK (fail_timeout_seconds BETWEEN 1 AND 3600),
  backup INTEGER NOT NULL DEFAULT 0 CHECK (backup IN (0, 1)),
  down INTEGER NOT NULL DEFAULT 0 CHECK (down IN (0, 1)),
  sort_order INTEGER NOT NULL DEFAULT 0 CHECK (sort_order BETWEEN -100000 AND 100000),
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(upstream_id, address COLLATE NOCASE),
  CHECK (NOT (backup = 1 AND down = 1))
);

CREATE INDEX idx_nginx_upstream_servers_order
  ON nginx_upstream_servers(upstream_id, sort_order, address COLLATE NOCASE);

CREATE TABLE nginx_upstream_applied_state (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  applied_hash TEXT NOT NULL DEFAULT '' CHECK (applied_hash = '' OR length(applied_hash) = 64),
  applied_path TEXT NOT NULL DEFAULT '',
  applied_at TEXT
);

INSERT INTO nginx_upstream_applied_state (id, applied_hash, applied_path, applied_at) VALUES (1, '', '', NULL);
