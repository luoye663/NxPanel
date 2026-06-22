-- 0001_init.sql — NxPanel 0.0.1 初始数据库基线

-- ============================================================
-- 迁移版本表
-- 记录已经执行过的迁移版本，RunMigrations 会根据这里跳过已执行文件。
-- ============================================================

CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 全局设置表
-- 用 key/value 保存面板运行时设置，例如 nginx 路径、默认站点、安全配置等。
-- ============================================================
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 管理员与会话
-- admin_account 只允许一个管理员账号；sessions 保存登录会话和 CSRF token 哈希。
-- 2FA 相关字段也在 admin_account 中保存，恢复码以 JSON 字符串形式存储。
-- ============================================================
CREATE TABLE IF NOT EXISTS admin_account (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  username TEXT NOT NULL UNIQUE DEFAULT 'admin',
  password_hash TEXT NOT NULL,
  password_algo TEXT NOT NULL DEFAULT 'bcrypt',
  totp_secret TEXT NOT NULL DEFAULT '',
  totp_enabled INTEGER NOT NULL DEFAULT 0,
  recovery_codes TEXT NOT NULL DEFAULT '[]',
  last_totp_code TEXT NOT NULL DEFAULT '',
  last_totp_time TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  csrf_token_hash TEXT NOT NULL,
  user_agent TEXT,
  ip TEXT,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);

-- ============================================================
-- 站点核心配置
-- sites 是站点主表；每个站点的域名、端口、根目录、日志路径和 marker 状态都在这里。
-- 其他站点功能表通过 site_id 关联到 sites，并在站点删除时级联清理。
-- ============================================================
CREATE TABLE IF NOT EXISTS sites (
  id TEXT PRIMARY KEY,
  primary_domain TEXT NOT NULL UNIQUE,
  domains_json TEXT NOT NULL,
  bindings_json TEXT,
  status TEXT NOT NULL CHECK (status IN ('enabled','disabled','failed','drifted','deleting')),
  http_port INTEGER NOT NULL DEFAULT 80,
  https_port INTEGER NOT NULL DEFAULT 443,
  root_path TEXT NOT NULL,
  index_files TEXT NOT NULL DEFAULT 'index.html index.htm',
  access_log_enabled INTEGER NOT NULL DEFAULT 1,
  access_log_path TEXT NOT NULL,
  error_log_path TEXT NOT NULL,
  config_path TEXT NOT NULL UNIQUE,
  enabled_path TEXT NOT NULL UNIQUE,
  rewrite_path TEXT NOT NULL UNIQUE,
  marker_version INTEGER NOT NULL DEFAULT 1,
  last_sync_warning TEXT NOT NULL DEFAULT '',
  access_limit_path TEXT,
  hotlink_path TEXT NOT NULL DEFAULT '',
  autoindex_enabled INTEGER NOT NULL DEFAULT 0,
  autoindex_exact_size INTEGER NOT NULL DEFAULT 0,
  autoindex_localtime INTEGER NOT NULL DEFAULT 1,
  autoindex_format TEXT NOT NULL DEFAULT 'html',
  error_page_404 TEXT NOT NULL DEFAULT '',
  error_page_403 TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_sites_status ON sites(status);
CREATE INDEX IF NOT EXISTS idx_sites_primary_domain ON sites(primary_domain);

-- ============================================================
-- 站点反向代理配置
-- 每条记录表示一个 location 反代规则，支持 WebSocket 和缓存参数。
-- ============================================================
CREATE TABLE IF NOT EXISTS site_proxy (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  location_path TEXT NOT NULL DEFAULT '/',
  upstream_url TEXT NOT NULL DEFAULT 'http://127.0.0.1:3000',
  host_header TEXT NOT NULL DEFAULT '$host',
  websocket_enabled INTEGER NOT NULL DEFAULT 0,
  connect_timeout INTEGER NOT NULL DEFAULT 60,
  send_timeout INTEGER NOT NULL DEFAULT 60,
  read_timeout INTEGER NOT NULL DEFAULT 60,
  cache_enabled INTEGER NOT NULL DEFAULT 0,
  cache_type TEXT NOT NULL DEFAULT 'nginx',
  cache_time INTEGER NOT NULL DEFAULT 60,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_site_proxy_site_id ON site_proxy(site_id);

-- ============================================================
-- 站点 SSL 配置
-- 保存证书路径、证书摘要、证书仓库引用和强制 HTTPS 等状态。
-- 私钥内容不进入 API 响应，表中只保存 key_path 和 key_sha256。
-- ============================================================
CREATE TABLE IF NOT EXISTS site_ssl (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 0,
  mode TEXT NOT NULL DEFAULT 'disabled' CHECK (mode IN ('disabled','manual_pem','existing_files','from_store')),
  cert_path TEXT,
  key_path TEXT,
  cert_sha256 TEXT,
  key_sha256 TEXT,
  issuer TEXT,
  subject TEXT,
  not_before TEXT,
  not_after TEXT,
  dns_names_json TEXT,
  force_https INTEGER NOT NULL DEFAULT 1,
  hsts_enabled INTEGER NOT NULL DEFAULT 0,
  cert_store_id TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_site_ssl_enabled ON site_ssl(enabled);

-- ============================================================
-- 站点伪静态配置元信息
-- 伪静态内容存储在文件系统中，数据库只记录内容 hash 和大小。
-- ============================================================
CREATE TABLE IF NOT EXISTS site_rewrite (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  content_hash TEXT,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ============================================================
-- 站点访问限制规则
-- 包含加密访问、禁止访问、IP 白名单/黑名单和防盗链规则。
-- 这些规则最终由 service 渲染为独立 include 文件或 marker 块。
-- ============================================================
CREATE TABLE IF NOT EXISTS site_auth_rules (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  path TEXT NOT NULL DEFAULT '/',
  username TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  htpasswd_path TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_site_auth_rules_site ON site_auth_rules(site_id);

CREATE TABLE IF NOT EXISTS site_deny_rules (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  deny_type TEXT NOT NULL DEFAULT 'extension',
  pattern TEXT NOT NULL DEFAULT '',
  extension_pattern TEXT,
  path_pattern TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_site_deny_rules_site ON site_deny_rules(site_id);

CREATE TABLE IF NOT EXISTS site_ip_whitelist_rules (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  rule_type TEXT NOT NULL DEFAULT 'allow',
  ips_json TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_site_ip_whitelist_rules_site ON site_ip_whitelist_rules(site_id);

CREATE TABLE IF NOT EXISTS site_hotlink_rules (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  extensions TEXT NOT NULL DEFAULT '',
  referers TEXT NOT NULL DEFAULT '',
  allow_empty_referer INTEGER NOT NULL DEFAULT 1,
  block_status INTEGER NOT NULL DEFAULT 403,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_site_hotlink_rules_site_id ON site_hotlink_rules(site_id);

-- ============================================================
-- 操作审计与文件备份
-- 所有写操作都会生成 operations 记录；写文件前的备份信息保存在 backups 中。
-- 这两张表用于审计、错误展示和必要时排查回滚过程。
-- ============================================================
CREATE TABLE IF NOT EXISTS operations (
  id TEXT PRIMARY KEY,
  action TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id TEXT,
  status TEXT NOT NULL CHECK (status IN ('pending','success','failed','rolled_back')),
  request_id TEXT,
  actor TEXT NOT NULL DEFAULT 'admin',
  ip TEXT,
  user_agent TEXT,
  message TEXT,
  error_code TEXT,
  error_message TEXT,
  stderr TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_operations_created_at ON operations(created_at);
CREATE INDEX IF NOT EXISTS idx_operations_target ON operations(target_type, target_id);

CREATE TABLE IF NOT EXISTS backups (
  id TEXT PRIMARY KEY,
  operation_id TEXT NOT NULL REFERENCES operations(id) ON DELETE CASCADE,
  file_path TEXT NOT NULL,
  backup_path TEXT NOT NULL,
  original_sha256 TEXT,
  backup_sha256 TEXT,
  file_existed INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_backups_operation_id ON backups(operation_id);

-- ============================================================
-- 证书仓库与 ACME
-- certificates 是用户证书仓库；acme_* 表保存 Let's Encrypt 账号、邮箱和订单状态。
-- ACME 账户私钥只保存在数据库中，不通过 API 暴露。
-- ============================================================
CREATE TABLE IF NOT EXISTS certificates (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  domains_json TEXT NOT NULL DEFAULT '[]',
  issuer TEXT,
  subject TEXT,
  not_before TEXT,
  not_after TEXT,
  cert_sha256 TEXT,
  key_sha256 TEXT,
  cert_path TEXT NOT NULL,
  key_path TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_certificates_cert_sha256 ON certificates(cert_sha256);
CREATE INDEX IF NOT EXISTS idx_certificates_not_after ON certificates(not_after);

CREATE TABLE IF NOT EXISTS acme_accounts (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  private_key_pem TEXT NOT NULL,
  directory_url TEXT NOT NULL DEFAULT 'https://acme-v02.api.letsencrypt.org/directory',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS acme_emails (
  email TEXT PRIMARY KEY,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS acme_orders (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  domains_json TEXT NOT NULL,
  challenge_type TEXT NOT NULL DEFAULT 'http-01',
  email TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending',
  certificate_id TEXT,
  error_type TEXT,
  error_detail TEXT,
  verification_url TEXT,
  verification_content TEXT,
  log_text TEXT,
  auto_renew INTEGER NOT NULL DEFAULT 1,
  last_renewed_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_acme_orders_site_id ON acme_orders(site_id);
CREATE INDEX IF NOT EXISTS idx_acme_orders_status ON acme_orders(status);
CREATE INDEX IF NOT EXISTS idx_acme_orders_auto_renew ON acme_orders(auto_renew, expires_at);

-- ============================================================
-- 登录审计
-- 记录登录成功/失败、来源 IP、User-Agent、CAPTCHA 和 2FA 使用情况。
-- 登录限流和安全排查会读取这里的数据。
-- ============================================================
CREATE TABLE IF NOT EXISTS login_audit (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL,
  ip TEXT NOT NULL,
  user_agent TEXT NOT NULL,
  success INTEGER NOT NULL DEFAULT 0,
  failure_reason TEXT NOT NULL DEFAULT '',
  captcha_verified INTEGER NOT NULL DEFAULT 0,
  totp_used INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);
CREATE INDEX IF NOT EXISTS idx_login_audit_ip ON login_audit(ip);
CREATE INDEX IF NOT EXISTS idx_login_audit_created ON login_audit(created_at);

-- ============================================================
-- 访问分析
-- 保存站点日志扫描设置、扫描任务、按天/小时/路径/IP 的聚合结果、明细和异常。
-- access_analysis_cursors 用于增量扫描时记录日志文件读取位置。
-- ============================================================
CREATE TABLE IF NOT EXISTS access_analysis_settings (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 0,
  scan_time TEXT NOT NULL DEFAULT '03:00',
  retention_days INTEGER NOT NULL DEFAULT 30,
  include_rotated INTEGER NOT NULL DEFAULT 0,
  log_format TEXT NOT NULL DEFAULT 'combined',
  custom_pattern TEXT,
  normalize_query INTEGER NOT NULL DEFAULT 0,
  save_entries INTEGER NOT NULL DEFAULT 0,
  entries_retention_days INTEGER NOT NULL DEFAULT 3,
  max_entries INTEGER NOT NULL DEFAULT 50000,
  path_top_n INTEGER NOT NULL DEFAULT 1000,
  ip_top_n INTEGER NOT NULL DEFAULT 1000,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS access_analysis_jobs (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  trigger TEXT NOT NULL,
  range_start TEXT NOT NULL,
  range_end TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('running','success','failed')),
  scanned_lines INTEGER NOT NULL DEFAULT 0,
  skipped_lines INTEGER NOT NULL DEFAULT 0,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  error_message TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_access_analysis_jobs_site_created ON access_analysis_jobs(site_id, created_at);

CREATE TABLE IF NOT EXISTS access_analysis_daily (
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  requests INTEGER NOT NULL DEFAULT 0,
  unique_ips INTEGER NOT NULL DEFAULT 0,
  status_2xx INTEGER NOT NULL DEFAULT 0,
  status_3xx INTEGER NOT NULL DEFAULT 0,
  status_4xx INTEGER NOT NULL DEFAULT 0,
  status_5xx INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (site_id, date)
);

CREATE TABLE IF NOT EXISTS access_analysis_hourly (
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  hour TEXT NOT NULL,
  requests INTEGER NOT NULL DEFAULT 0,
  unique_ips INTEGER NOT NULL DEFAULT 0,
  status_4xx INTEGER NOT NULL DEFAULT 0,
  status_5xx INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (site_id, hour)
);

CREATE TABLE IF NOT EXISTS access_analysis_paths (
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  path TEXT NOT NULL,
  requests INTEGER NOT NULL DEFAULT 0,
  unique_ips INTEGER NOT NULL DEFAULT 0,
  status_2xx INTEGER NOT NULL DEFAULT 0,
  status_3xx INTEGER NOT NULL DEFAULT 0,
  status_4xx INTEGER NOT NULL DEFAULT 0,
  status_5xx INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  last_seen_at TEXT NOT NULL,
  PRIMARY KEY (site_id, date, path)
);
CREATE INDEX IF NOT EXISTS idx_access_analysis_paths_rank ON access_analysis_paths(site_id, date, requests);

CREATE TABLE IF NOT EXISTS access_analysis_ips (
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  ip TEXT NOT NULL,
  requests INTEGER NOT NULL DEFAULT 0,
  unique_paths INTEGER NOT NULL DEFAULT 0,
  error_requests INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  sample_user_agent TEXT,
  PRIMARY KEY (site_id, date, ip)
);
CREATE INDEX IF NOT EXISTS idx_access_analysis_ips_rank ON access_analysis_ips(site_id, date, requests);

CREATE TABLE IF NOT EXISTS access_analysis_entries (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  ts TEXT NOT NULL,
  ip TEXT NOT NULL,
  method TEXT,
  path TEXT NOT NULL,
  raw_path TEXT,
  status INTEGER NOT NULL DEFAULT 0,
  bytes INTEGER NOT NULL DEFAULT 0,
  referer TEXT,
  user_agent TEXT,
  is_anomaly INTEGER NOT NULL DEFAULT 0,
  anomaly_reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_access_analysis_entries_site_ts ON access_analysis_entries(site_id, ts);
CREATE INDEX IF NOT EXISTS idx_access_analysis_entries_site_path ON access_analysis_entries(site_id, path);
CREATE INDEX IF NOT EXISTS idx_access_analysis_entries_site_ip ON access_analysis_entries(site_id, ip);

CREATE TABLE IF NOT EXISTS access_analysis_anomalies (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  date TEXT NOT NULL,
  kind TEXT NOT NULL,
  target TEXT NOT NULL,
  requests INTEGER NOT NULL DEFAULT 0,
  severity TEXT NOT NULL DEFAULT 'medium',
  reason TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_access_analysis_anomalies_site_date ON access_analysis_anomalies(site_id, date);

CREATE TABLE IF NOT EXISTS access_analysis_cursors (
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  log_path TEXT NOT NULL,
  inode INTEGER NOT NULL DEFAULT 0,
  offset INTEGER NOT NULL DEFAULT 0,
  file_size INTEGER NOT NULL DEFAULT 0,
  last_scan_at TEXT,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (site_id, log_path)
);

-- ============================================================
-- 站点备份
-- site_backups 保存用户手动或计划任务生成的备份元数据。
-- site_backup_schedules 是旧版定时备份兼容表，启动时会迁移到统一计划任务中心。
-- ============================================================
CREATE TABLE IF NOT EXISTS site_backups (
  id TEXT PRIMARY KEY,
  site_id TEXT NOT NULL REFERENCES sites(id) ON DELETE CASCADE,
  backup_type TEXT NOT NULL CHECK (backup_type IN ('config','root','ssl','full')),
  name TEXT NOT NULL,
  backup_path TEXT NOT NULL,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','success','failed','deleted')),
  message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_site_backups_site_id_created ON site_backups(site_id, created_at DESC);

CREATE TABLE IF NOT EXISTS site_backup_schedules (
  site_id TEXT PRIMARY KEY REFERENCES sites(id) ON DELETE CASCADE,
  enabled INTEGER NOT NULL DEFAULT 0,
  backup_type TEXT NOT NULL DEFAULT 'full' CHECK (backup_type IN ('config','root','ssl','full')),
  backup_dir TEXT NOT NULL DEFAULT '',
  retention_count INTEGER NOT NULL DEFAULT 7,
  schedule_type TEXT NOT NULL DEFAULT 'daily' CHECK (schedule_type IN ('daily','weekly','monthly')),
  schedule_time TEXT NOT NULL DEFAULT '02:00',
  weekday INTEGER NOT NULL DEFAULT 1,
  month_day INTEGER NOT NULL DEFAULT 1,
  last_run_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_site_backup_schedules_enabled ON site_backup_schedules(enabled);

-- ============================================================
-- 统一计划任务中心
-- scheduled_tasks 保存任务定义；scheduled_task_runs 保存每次执行历史。
-- 系统任务包括 ACME 自动续签、站点备份、访问分析扫描和 Nginx 日志切割等。
-- ============================================================
CREATE TABLE IF NOT EXISTS scheduled_tasks (
  id TEXT PRIMARY KEY,
  type TEXT NOT NULL,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  system INTEGER NOT NULL DEFAULT 0,
  source_type TEXT NOT NULL DEFAULT '',
  source_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'idle',
  schedule_kind TEXT NOT NULL,
  schedule_expr TEXT NOT NULL,
  timezone TEXT NOT NULL DEFAULT 'UTC',
  params_json TEXT NOT NULL DEFAULT '{}',
  concurrency_policy TEXT NOT NULL DEFAULT 'skip',
  missed_policy TEXT NOT NULL DEFAULT 'run_once',
  timeout_seconds INTEGER NOT NULL DEFAULT 600,
  max_retries INTEGER NOT NULL DEFAULT 0,
  retry_delay_seconds INTEGER NOT NULL DEFAULT 60,
  next_run_at TEXT,
  last_run_at TEXT,
  last_finished_at TEXT,
  last_status TEXT,
  last_error TEXT NOT NULL DEFAULT '',
  last_duration_ms INTEGER NOT NULL DEFAULT 0,
  last_run_id TEXT NOT NULL DEFAULT '',
  locked_by TEXT NOT NULL DEFAULT '',
  locked_until TEXT,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_enabled_next ON scheduled_tasks(enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_status ON scheduled_tasks(status);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_type ON scheduled_tasks(type);
CREATE INDEX IF NOT EXISTS idx_scheduled_tasks_lock ON scheduled_tasks(status, locked_until);
CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduled_tasks_source ON scheduled_tasks(source_type, source_id)
  WHERE source_type != '' AND source_id != '';

CREATE TABLE IF NOT EXISTS scheduled_task_runs (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
  task_type TEXT NOT NULL,
  task_name TEXT NOT NULL,
  trigger TEXT NOT NULL,
  status TEXT NOT NULL,
  attempt INTEGER NOT NULL DEFAULT 1,
  started_at TEXT NOT NULL,
  finished_at TEXT,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  log_file TEXT NOT NULL DEFAULT '',
  operation_id TEXT NOT NULL DEFAULT '',
  request_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_runs_task_created ON scheduled_task_runs(task_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_runs_type_created ON scheduled_task_runs(task_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_runs_status ON scheduled_task_runs(status);

-- ============================================================
-- 伪静态模板
-- rewrite_templates 保存内置模板和用户自定义模板，前端可动态增删改。
-- 下面的 INSERT 初始化首版内置模板，时间固定为 1970 方便测试和重复比较。
-- ============================================================
CREATE TABLE IF NOT EXISTS rewrite_templates (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  params_json TEXT NOT NULL DEFAULT '[]',
  template TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rewrite_templates_enabled_sort ON rewrite_templates(enabled, sort_order);

INSERT INTO rewrite_templates
  (id, name, category, description, params_json, template, enabled, sort_order, created_at, updated_at)
VALUES
('spa',
 'React/Vue SPA',
 'static',
 '单页应用刷新路由回退到 index.html。',
 '[]',
 'location / {
    try_files $uri $uri/ /index.html;
}
',
 1, 0, '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z'),
('docker-proxy',
 'Docker 容器反代',
 'proxy',
 '将当前站点根路径反代到本机容器端口。',
 '[{"key":"upstream_url","label":"目标地址","type":"string","default":"http://127.0.0.1:8080","required":true},{"key":"pass_real_ip","label":"传递真实 IP","type":"boolean","default":true},{"key":"websocket","label":"WebSocket","type":"boolean","default":false}]',
 'location / {
    proxy_pass {{ .upstream_url }};
    proxy_set_header Host $host;
{{- if .pass_real_ip }}
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
{{- end }}
{{- if .websocket }}
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
{{- end }}
}
',
 1, 1, '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z'),
('docker-proxy-sse',
 'Docker 容器反代（SSE）',
 'proxy',
 '将当前站点反代到本机容器端口，并针对 Server-Sent Events 关闭缓冲、加长读超时。',
 '[{"key":"upstream_url","label":"目标地址","type":"string","default":"http://127.0.0.1:8080","required":true},{"key":"pass_real_ip","label":"传递真实 IP","type":"boolean","default":true},{"key":"websocket","label":"WebSocket","type":"boolean","default":false},{"key":"read_timeout","label":"读超时(秒)","type":"number","default":3600,"required":true}]',
 'location / {
    proxy_pass {{ .upstream_url }};
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header Connection "";
{{- if .pass_real_ip }}
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
{{- end }}
{{- if .websocket }}
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
{{- end }}
    # SSE：关闭缓冲并加长读超时，否则事件流会被攒批或超时断开
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout {{ .read_timeout }}s;
}
',
 1, 2, '1970-01-01T00:00:00Z', '1970-01-01T00:00:00Z');
