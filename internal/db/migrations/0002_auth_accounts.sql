-- ============================================================
-- 统一访问账户池
-- ============================================================

CREATE TABLE IF NOT EXISTS auth_accounts (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL CHECK (scope IN ('global','site')),
  site_id TEXT REFERENCES sites(id) ON DELETE CASCADE,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK ((scope = 'global' AND site_id IS NULL) OR (scope = 'site' AND site_id IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS idx_auth_accounts_scope_site ON auth_accounts(scope, site_id);

CREATE TABLE IF NOT EXISTS site_auth_rule_accounts (
  rule_id TEXT NOT NULL REFERENCES site_auth_rules(id) ON DELETE CASCADE,
  account_id TEXT NOT NULL REFERENCES auth_accounts(id) ON DELETE RESTRICT,
  PRIMARY KEY (rule_id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_site_auth_rule_accounts_account ON site_auth_rule_accounts(account_id);

ALTER TABLE site_proxy ADD COLUMN auth_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE site_proxy ADD COLUMN auth_htpasswd_path TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS site_proxy_auth_accounts (
  proxy_id TEXT NOT NULL REFERENCES site_proxy(id) ON DELETE CASCADE,
  account_id TEXT NOT NULL REFERENCES auth_accounts(id) ON DELETE RESTRICT,
  PRIMARY KEY (proxy_id, account_id)
);
CREATE INDEX IF NOT EXISTS idx_site_proxy_auth_accounts_account ON site_proxy_auth_accounts(account_id);

-- 旧加密访问规则每条规则自带一个账号，迁移为站点账户并建立规则关联。
-- 旧表未限制 username 唯一；重复用户名会按规则 ID 加后缀，避免不同密码被错误合并。
CREATE TEMP TABLE nx_auth_account_migration AS
SELECT
  id AS rule_id,
  site_id,
  CASE
    WHEN ROW_NUMBER() OVER (PARTITION BY username ORDER BY created_at, id) = 1 THEN username
    ELSE username || '_' || id
  END AS migrated_username,
  password_hash,
  created_at,
  updated_at
FROM site_auth_rules
WHERE username IS NOT NULL AND username != '';

INSERT INTO auth_accounts (id, scope, site_id, username, password_hash, enabled, created_at, updated_at)
SELECT 'aa_' || substr(lower(hex(randomblob(16))), 1, 16), 'site', site_id, migrated_username, password_hash, 1, created_at, updated_at
FROM nx_auth_account_migration;

INSERT INTO site_auth_rule_accounts (rule_id, account_id)
SELECT m.rule_id, a.id
FROM nx_auth_account_migration m
JOIN auth_accounts a ON a.username = m.migrated_username;

DROP TABLE nx_auth_account_migration;
