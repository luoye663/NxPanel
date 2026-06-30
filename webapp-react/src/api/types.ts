// API 类型定义
// 字段名必须与后端契约保持一致，尤其不要把 snake_case 改成 camelCase。

// === 分页 ===
export interface PaginatedData<T> {
  items: T[]
  page: number
  page_size: number
  total: number
}

// === Auth ===
export interface SetupAdminRequest {
  username: string
  password: string
  login_path?: string
  captcha_provider?: string
  captcha_site_key?: string
  captcha_secret_key?: string
  captcha_trigger_after_failures?: number
}

export interface LoginRequest {
  username: string
  password: string
  captcha_token?: string
}

export interface CaptchaConfigResponse {
  required: boolean
  provider?: string
  site_key?: string
}

export interface LoginResponse {
  username: string
  requires_2fa: boolean
  temp_token?: string
}

export interface Login2FARequest {
  temp_token: string
  code: string
}

export interface LoginRecoverRequest {
  temp_token: string
  recovery_code: string
}

export interface AuthMeResponse {
  authenticated: boolean
  username: string
  needs_setup: boolean
  totp_enabled: boolean
}

export interface TwoFAStatusResponse {
  enabled: boolean
  has_recovery: boolean
}

export interface TwoFASetupResponse {
  secret: string
  url: string
}

export interface TwoFAEnableRequest {
  code: string
}

export interface TwoFAEnableResponse {
  recovery_codes: string[]
}

export interface TwoFADisableRequest {
  code: string
}

export interface ChangePasswordRequest {
  current_password: string
  new_password: string
}

// === System ===
export interface SystemOverview {
  api: {
    version: string
    user: string
  }
  agent: {
    available: boolean
    version: string
    socket: string
  }
  nginx: {
    detected: boolean
    bin: string
    version: string
    conf_path: string
    running: boolean
    include_installed: boolean
  }
}

export interface ProcessInfo {
  pid: number
  name: string
  command: string
  cpu_percent: number
  mem_percent: number
  rss_bytes: number
}

export interface SystemMetricsSnapshot {
  timestamp: number
  system: {
    name: string
    pretty_name: string
    uptime_seconds: number
  }
  load: {
    load1: number
    load5: number
    load15: number
    running_processes: number
    total_processes: number
    percent: number
  }
  cpu: {
    percent: number
    times: Record<string, number>
    cores: { name: string; percent: number }[]
    info: {
      model: string
      physical_cpus: number
      physical_cores: number
      logical_cores: number
    }
  }
  memory: {
    total_bytes: number
    used_bytes: number
    free_bytes: number
    available_bytes: number
    shared_bytes: number
    buffers_bytes: number
    cached_bytes: number
    percent: number
  }
  disks: {
    device: string
    mountpoint: string
    fs_type: string
    usage_key: string
    counted: boolean
    duplicate_of?: string
    total_bytes: number
    used_bytes: number
    free_bytes: number
    avail_bytes: number
    percent: number
    inodes: { total: number; used: number; free: number; percent: number }
  }[]
  network: {
    interfaces: { name: string; rx_bytes: number; tx_bytes: number; rx_bytes_per_sec: number; tx_bytes_per_sec: number }[]
    total: { name: string; rx_bytes: number; tx_bytes: number; rx_bytes_per_sec: number; tx_bytes_per_sec: number }
  }
  disk_io: {
    devices: { name: string; read_bytes: number; write_bytes: number; read_bytes_per_sec: number; write_bytes_per_sec: number; iops: number; latency_ms: number }[]
    total: { name: string; read_bytes: number; write_bytes: number; read_bytes_per_sec: number; write_bytes_per_sec: number; iops: number; latency_ms: number }
  }
  top: {
    cpu: ProcessInfo[]
    memory: ProcessInfo[]
  }
}

export interface Binding {
  domain: string
  port: number
}

// === Sites ===
export interface SiteListItem {
  id: string
  primary_domain: string
  domains: string[]
  bindings: Binding[]
  status: string
  root_path: string
  ssl_enabled: boolean
  proxy_enabled: boolean
  access_log_path: string
  error_log_path: string
  updated_at: string
}

export interface CreateSiteRequest {
  bindings: Binding[]
  root_path: string
  index_files: string
  access_log_enabled: boolean
  create_root: boolean
  create_index: boolean
  enable_after_create: boolean
}

export interface MarkerStatus {
  valid: boolean
  missing: string[]
  duplicated: string[]
}

export interface SiteDetail {
  id: string
  primary_domain: string
  domains: string[]
  bindings: Binding[]
  status: string
  http_port: number
  https_port: number
  config_path: string
  root_path: string
  index_files: string
  index_file_list: string[]
  autoindex_enabled: boolean
  autoindex_exact_size: boolean
  autoindex_localtime: boolean
  autoindex_format: string
  error_page_404: string
  error_page_403: string
  access_log_enabled: boolean
  access_log_path: string
  error_log_path: string
  is_imported: boolean
  marker_status: MarkerStatus
  import_warnings?: string[]
  proxy: SiteProxy | null
  ssl: { enabled: boolean; mode: string; [key: string]: unknown }
}

export interface SiteBackup {
  id: string
  site_id: string
  backup_type: string
  name: string
  size_bytes: number
  status: string
  message: string
  created_at: string
  updated_at: string
  finished_at?: string | null
}

export interface SiteBackupListResponse {
  items: SiteBackup[]
}

export interface SiteBackupCreateRequest {
  backup_type: string
  name?: string
  backup_dir?: string
}

export interface SiteBackupRestoreRequest {
  restore_config: boolean
  restore_root: boolean
  restore_ssl: boolean
}

export interface SiteBackupTask {
  task_id: string
  stream_id: string
  status: string
  action: string
  backup?: SiteBackup
  message: string
  error?: string
  created_at: string
  updated_at: string
}

export interface SiteBackupSchedule {
  enabled: boolean
  backup_type: string
  backup_dir: string
  retention_count: number
  schedule_type: string
  schedule_time: string
  weekday: number
  month_day: number
  last_run_at?: string | null
}

export type SiteBackupScheduleSaveRequest = Omit<SiteBackupSchedule, 'last_run_at'>

// === Access Analysis ===
export interface AccessAnalysisSummary {
  today_requests: number
  unique_ips: number
  status_4xx: number
  status_5xx: number
  bytes: number
  top_path: string
  last_scan_at: string
  last_job_status: string
  last_error: string
}

export interface AccessAnalysisHourlyPoint {
  hour: string
  requests: number
  unique_ips: number
  status_4xx: number
  status_5xx: number
  bytes: number
}

export interface AccessAnalysisSummaryResponse {
  summary: AccessAnalysisSummary
  trend: AccessAnalysisHourlyPoint[]
}

export interface AccessAnalysisSettings {
  site_id: string
  enabled: boolean
  scan_time: string
  retention_days: number
  include_rotated: boolean
  log_format: string
  custom_pattern: string
  normalize_query: boolean
  save_entries: boolean
  entries_retention_days: number
  max_entries: number
  path_top_n: number
  ip_top_n: number
  created_at: string
  updated_at: string
}

export interface AccessAnalysisScanRequest {
  range: string
  from?: string
  to?: string
  include_rotated?: boolean
}

export interface AccessAnalysisScanResponse {
  job_id: string
  status: string
  scanned_lines: number
  skipped_lines: number
  truncated: boolean
  duration_ms: number
}

export interface AccessPathStat {
  date: string
  path: string
  requests: number
  unique_ips: number
  status_2xx: number
  status_3xx: number
  status_4xx: number
  status_5xx: number
  bytes: number
  last_seen_at: string
}

export interface AccessIPStat {
  date: string
  ip: string
  requests: number
  unique_paths: number
  error_requests: number
  bytes: number
  first_seen_at: string
  last_seen_at: string
  sample_user_agent: string
}

export interface AccessEntry {
  id: number
  ts: string
  ip: string
  method: string
  path: string
  raw_path: string
  status: number
  bytes: number
  referer: string
  user_agent: string
  is_anomaly: boolean
  anomaly_reason: string
}

export interface AccessAnomaly {
  id: number
  date: string
  kind: string
  target: string
  requests: number
  severity: string
  reason: string
  first_seen_at: string
  last_seen_at: string
}

export interface AccessAnalysisJob {
  id: string
  site_id: string
  trigger: string
  range_start: string
  range_end: string
  status: string
  scanned_lines: number
  skipped_lines: number
  duration_ms: number
  error_message: string
  created_at: string
  finished_at: string
}

export interface AccessAnalysisFormatDetectResponse {
  format: string
  parseable: boolean
  failure_rate: number
  samples: AccessEntry[]
  recommended_conf: string
  errors: string[]
}

// === Files ===
export interface FileEntry {
  name: string
  size: number
  mod_time: string
  is_dir: boolean
  mode: string
  owner: string
  group: string
}

export interface FileListResponse {
  path: string
  entries: FileEntry[]
}

export interface FileReadResponse {
  path: string
  content_base64: string
  size: number
}

// === Operations ===
export interface OperationItem {
  id: string
  action: string
  target_type: string
  target_id: string
  status: string
  message: string
  created_at: string
  finished_at: string
}

// === Login Audit ===
export interface LoginAuditItem {
  id: number
  username: string
  ip: string
  user_agent: string
  success: boolean
  failure_reason: string
  captcha_verified: boolean
  totp_used: boolean
  created_at: string
}

// === Service Logs ===
export interface ServiceLogResponse {
  lines: string[]
  truncated: boolean
  path?: string
}

// === Task Logs ===
export interface TaskLogEntry {
  name: string
  type: string
  label: string
  size: number
  mod_time: string
}

export interface TaskLogTypeList {
  tasks: TaskLogEntry[]
}

// === Settings — 高级配置 ===
export interface DefaultPagesSettings {
  new_site_page: string
  page_404: string
  site_not_found_page: string
  site_disabled_page: string
}

export interface DefaultSiteSettings {
  site_id: string | null
  primary_domain: string | null
}

export interface HTTPSHijackSettings {
  enabled: boolean
  return_status_code: number
  cert_mode: 'self_signed' | 'custom'
  custom_cert_id: string | null
  cert_path: string | null
  key_path: string | null
}

export interface LogRotateSettings {
  enabled: boolean
  interval: string
  max_count: number
  max_age: string
  min_size: string
}

export type UpdateLogRotateRequest = Partial<Omit<LogRotateSettings, 'enabled'>> & {
  enabled?: boolean
}

// === Security Settings — API 安全配置 ===
export interface SecuritySettings {
  login_path: string
  public_health: boolean
  rate_limit_max_failures: number
  rate_limit_window: string
  max_sessions: number
  bind_session_ip: boolean
  bind_session_ua: boolean
  trusted_proxies: string[]
  captcha_provider: string
  captcha_site_key: string
  captcha_secret_key_masked: string
  captcha_trigger_after_failures: number
  tls_enabled: boolean
  tls_cert: string
  tls_key: string
  tls_cert_validity: string
}

export type UpdateSecuritySettingsRequest = Partial<{
  login_path: string
  public_health: boolean
  rate_limit_max_failures: number
  rate_limit_window: string
  max_sessions: number
  bind_session_ip: boolean
  bind_session_ua: boolean
  trusted_proxies: string[]
  captcha_provider: string
  captcha_site_key: string
  captcha_secret_key: string
  captcha_trigger_after_failures: number
  tls_enabled: boolean
  tls_cert: string
  tls_key: string
  tls_cert_validity: string
}>

// === Nginx Conf ===
export interface NginxConfResponse {
  content: string
  hash: string
}

export interface SaveNginxConfResponse {
  hash: string
  operation_id: string
}

export interface NginxParameterValue {
  key: string
  value: string
  default_value: string
  description: string
  unit: string
  group: string
  tooltip: string
  options?: string[]
  clearable?: boolean
}

export interface NginxParametersResponse {
  parameters: NginxParameterValue[]
  conf_path: string
}

export interface SaveNginxParametersResponse {
  parameters: NginxParameterValue[]
  conf_path: string
  operation_id: string
}

// === Proxy — 反向代理 ===
export interface SiteProxy {
  id: string
  name: string
  enabled: boolean
  location_path: string
  upstream_url: string
  host_header: string
  websocket_enabled: boolean
  connect_timeout: number
  send_timeout: number
  read_timeout: number
  cache_enabled: boolean
  cache_type: 'nginx' | 'file'
  cache_time: number
  auth_enabled: boolean
  auth_account_ids: string[]
  auth_accounts?: { id: string; scope: 'global' | 'site'; site_id?: string; username: string; enabled: boolean }[]
}

export interface CreateProxyRequest {
  name: string
  enabled: boolean
  location_path: string
  upstream_url: string
  host_header: string
  websocket_enabled: boolean
  connect_timeout: number
  send_timeout: number
  read_timeout: number
  cache_enabled: boolean
  cache_type: 'nginx' | 'file'
  cache_time: number
  auth_enabled?: boolean
  auth_account_ids?: string[]
}

export interface UpdateProxyRequest extends CreateProxyRequest {}

// === Upgrade ===
export interface UpgradeStatus {
  has_upgrade: boolean
  current_version: string
  latest_version: string
  release_url: string
  published_at: string
  body: string
  checked_at: string
  error: string
}
