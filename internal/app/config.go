// app 包包含全局配置、日志、ID 生成等基础工具
//
// 本文件负责：
// 1. 定义 Config 结构体（对应 config.yaml）
// 2. 从 YAML 文件加载配置（gopkg.in/yaml.v3）
// 3. 支持环境变量覆盖（NXPANEL_ 前缀）
// 4. slog 结构化日志初始化
package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ============================================================
// 配置结构体定义
// ============================================================

// Config 是面板的全局配置结构，对应 config.yaml 文件
type Config struct {
	// LogLevel 日志级别：debug / info / warn / error
	LogLevel string `yaml:"log_level"`

	// LogFormat 日志格式：json（默认，生产推荐）或 text（开发调试方便阅读）
	LogFormat string `yaml:"log_format"`

	// LogFile 兼容旧版单一日志文件路径；新部署推荐使用 ServiceLogs 分别配置 API/Agent
	LogFile string `yaml:"log_file"`

	// ServiceLogs API/Agent 独立运行日志路径，避免两个进程共用顶层 log_file
	ServiceLogs ServiceLogsConfig `yaml:"service_logs"`

	// ServiceLogRotate API/Agent 自身运行日志切割配置，独立于 Nginx 站点日志切割
	ServiceLogRotate ServiceLogRotateConfig `yaml:"service_log_rotate"`

	// DataDir 数据目录，存放 SQLite 数据库
	DataDir string `yaml:"data_dir"`

	ConfigFile string `yaml:"-"`
	// API 配置块
	API APIConfig `yaml:"api"`

	// Agent 配置块
	Agent AgentConfig `yaml:"agent"`

	// Nginx 配置块
	Nginx NginxConfig `yaml:"nginx"`

	// ACME 配置块
	ACME ACMEConfig `yaml:"acme"`

	// Database 配置块
	Database DatabaseConfig `yaml:"database"`

	// Upgrade 升级检测配置块
	Upgrade UpgradeConfig `yaml:"upgrade"`
}

// ServiceLogsConfig — API/Agent 自身运行日志路径配置
type ServiceLogsConfig struct {
	APILogFile   string `yaml:"api_log_file"`
	AgentLogFile string `yaml:"agent_log_file"`
}

// ServiceLogRotateConfig — API/Agent 自身运行日志切割配置
type ServiceLogRotateConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Interval string `yaml:"interval"`
	MaxSize  string `yaml:"max_size"`
	MaxCount int    `yaml:"max_count"`
	MaxAge   string `yaml:"max_age"`
}

// DatabaseConfig — 数据库相关配置
type DatabaseConfig struct {
	// BusyTimeout SQLite 忙等超时（毫秒）
	BusyTimeout int `yaml:"busy_timeout"`
}

// UpgradeConfig — 升级检测配置
type UpgradeConfig struct {
	// Enabled 是否启用升级检测
	Enabled bool `yaml:"enabled"`

	// CheckInterval 检查间隔（如 "6h"）
	CheckInterval string `yaml:"check_interval"`

	// GitHubRepo GitHub 仓库（owner/repo 格式）
	GitHubRepo string `yaml:"github_repo"`
}

// TLSConfig — API TLS 配置
type TLSConfig struct {
	Enabled      bool     `yaml:"enabled"`
	Cert         string   `yaml:"cert"`
	Key          string   `yaml:"key"`
	CertValidity string   `yaml:"cert_validity"`
	SANs         []string `yaml:"sans"`
}

// APIConfig — API 服务的配置
type APIConfig struct {
	Listen string `yaml:"listen"`

	LoginPath string `yaml:"login_path"`

	PublicHealth bool `yaml:"public_health"`

	SessionDuration string `yaml:"session_duration"`

	ReadTimeout string `yaml:"read_timeout"`

	WriteTimeout string `yaml:"write_timeout"`

	IdleTimeout string `yaml:"idle_timeout"`

	ShutdownTimeout string `yaml:"shutdown_timeout"`

	SSEHeartbeat string `yaml:"sse_heartbeat"`

	SystemMetricsInterval string `yaml:"system_metrics_interval"`

	UploadTimeout string `yaml:"upload_timeout"`

	RateLimit RateLimitConfig `yaml:"rate_limit"`

	TrustedProxies []string `yaml:"trusted_proxies"`

	MaxSessions int `yaml:"max_sessions"`

	Captcha CaptchaConfig `yaml:"captcha"`

	BindSessionIP bool `yaml:"bind_session_ip"`

	BindSessionUA bool `yaml:"bind_session_ua"`

	TLS TLSConfig `yaml:"tls"`
}

type RateLimitConfig struct {
	MaxFailures int    `yaml:"max_failures"`
	Window      string `yaml:"window"`
}

type CaptchaConfig struct {
	Provider             string `yaml:"provider"`
	SiteKey              string `yaml:"site_key"`
	SecretKey            string `yaml:"secret_key"`
	TriggerAfterFailures int    `yaml:"trigger_after_failures"`
}

// AgentConfig — Agent 服务的配置
type AgentConfig struct {
	// SocketPath Unix Socket 路径
	SocketPath string `yaml:"socket_path"`

	// SocketGroup socket 文件的所属组，用于允许非 root 的 API 进程连接
	// 为空时不修改 socket 文件的 group（适用于 root 单用户运行场景）
	SocketGroup string `yaml:"socket_group"`

	// Token agent 认证密钥，API 和 agent 共享
	// 生产环境必须通过环境变量 NXPANEL_AGENT_TOKEN 设置强随机字符串
	Token string `yaml:"token"`

	// AllowedRoots 追加的文件操作白名单路径（可选）
	// 这些路径会与配置驱动路径和硬编码通用路径合并
	AllowedRoots []string `yaml:"allowed_roots"`

	// ShutdownTimeout 优雅关闭超时（如 "10s"）
	ShutdownTimeout string `yaml:"shutdown_timeout"`

	// ClientTimeout API → Agent HTTP 客户端总超时（如 "30s"）
	ClientTimeout string `yaml:"client_timeout"`

	// DialTimeout Unix Socket 拨号超时（如 "3s"）
	DialTimeout string `yaml:"dial_timeout"`

	// IdleConnTimeout 空闲连接超时（如 "30s"）
	IdleConnTimeout string `yaml:"idle_conn_timeout"`

	// MaxReadSize 文件读取、日志 tail 的最大内存读取量（如 "16M"）
	MaxReadSize string `yaml:"max_read_size"`

	// MaxDownloadSize 文件/日志下载的最大允许大小（如 "256M"）
	MaxDownloadSize string `yaml:"max_download_size"`

	// DownloadTimeout 单次文件/日志下载最长传输时间（如 "2m"）
	DownloadTimeout string `yaml:"download_timeout"`
}

// NginxConfig — Nginx 相关配置
type NginxConfig struct {
	Bin string `yaml:"bin"`

	ConfPath string `yaml:"conf_path"`

	PanelDir string `yaml:"panel_dir"`

	TemplatesDir string `yaml:"templates_dir"`

	WebUser string `yaml:"web_user"`

	WebGroup string `yaml:"web_group"`

	Version string `yaml:"version"`

	IncludeInstalled bool `yaml:"include_installed"`

	// LogDir 默认日志目录，用于生成站点默认日志路径
	LogDir string `yaml:"log_dir"`

	// AllowedRootPrefixes 允许的网站根目录前缀白名单
	AllowedRootPrefixes []string `yaml:"allowed_root_prefixes"`

	// AllowedLogPrefixes 允许的日志文件路径前缀白名单
	AllowedLogPrefixes []string `yaml:"allowed_log_prefixes"`

	// WebUserCandidates 自动检测 web 用户时的候选列表
	WebUserCandidates []string `yaml:"web_user_candidates"`

	// TestTimeout nginx -t 超时
	TestTimeout string `yaml:"test_timeout"`

	// ReloadTimeout nginx -s reload 超时
	ReloadTimeout string `yaml:"reload_timeout"`

	// DumpTimeout nginx -T 超时
	DumpTimeout string `yaml:"dump_timeout"`

	// DetectTimeout nginx -V 检测超时
	DetectTimeout string `yaml:"detect_timeout"`

	// BackupMaxCount 备份目录最大保留数量，超过此数量且超过 BackupMaxAge 的备份会被清理
	BackupMaxCount int `yaml:"backup_max_count"`

	// BackupMaxAge 备份目录最小保留时间，超过此时间且数量超过 BackupMaxCount 的备份会被清理
	BackupMaxAge string `yaml:"backup_max_age"`
}

// ACMEConfig — ACME/Let's Encrypt 相关配置
type ACMEConfig struct {
	// UseStaging 使用 Let's Encrypt 测试环境
	UseStaging bool `yaml:"use_staging"`

	// AutoRenewDays 是 ACME 续签系统任务的缺省参数，不再从 YAML 读取。
	AutoRenewDays int `yaml:"-"`

	// PreValidation HTTP-01 本机预验证配置
	PreValidation PreValidationConfig `yaml:"pre_validation"`
}

// PreValidationConfig — HTTP-01 预验证配置
type PreValidationConfig struct {
	// DNSServer DNS 服务器，空=系统默认，支持 "8.8.8.8:53" 或 "https://1.1.1.1/dns-query"（DoH）
	DNSServer string `yaml:"dns_server"`

	// RetryInterval 验证失败后重试间隔（如 "3s"）
	RetryInterval string `yaml:"retry_interval"`

	// RetryCount 最大重试次数
	RetryCount int `yaml:"retry_count"`
}

// ============================================================
// 配置加载
// ============================================================

// LoadConfig 从 YAML 文件加载配置，并支持环境变量覆盖
//
// 加载顺序：
//  1. 使用默认值初始化
//  2. 读取 YAML 文件覆盖默认值（文件不存在则跳过）
//  3. 环境变量覆盖 YAML 中的值
//
// 环境变量格式：NXPANEL_{YAML键名大写}
// 例如：NXPANEL_API_LISTEN、NXPANEL_AGENT_SOCKET_PATH
func LoadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	absPath, _ := filepath.Abs(path)
	cfg.ConfigFile = absPath

	// 读取 YAML 文件
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在是合法的，使用默认值 + 环境变量
			applyEnvOverrides(cfg)
			normalizeLoginPath(cfg)
			return cfg, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 使用 yaml.v3 正式解析 YAML
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 环境变量覆盖 YAML 配置（优先级最高）
	applyEnvOverrides(cfg)
	normalizeLoginPath(cfg)

	return cfg, nil
}

// DefaultConfig 返回包含合理默认值的配置（公共接口，用于配置迁移）
func DefaultConfig() *Config {
	return defaultConfig()
}

// defaultConfig 返回包含合理默认值的配置
func defaultConfig() *Config {
	return &Config{
		LogLevel:  "info",
		LogFormat: "json",
		DataDir:   "/opt/nxpanel/data",
		ServiceLogRotate: ServiceLogRotateConfig{
			Enabled:  true,
			Interval: "1h",
			MaxSize:  "50M",
			MaxCount: 30,
			MaxAge:   "720h",
		},
		API: APIConfig{
			Listen:                "127.0.0.1:8888",
			SessionDuration:       "24h",
			ReadTimeout:           "15s",
			WriteTimeout:          "30s",
			IdleTimeout:           "60s",
			ShutdownTimeout:       "10s",
			SSEHeartbeat:          "15s",
			SystemMetricsInterval: "2s",
			UploadTimeout:         "300s",
			RateLimit: RateLimitConfig{
				MaxFailures: 5,
				Window:      "15m",
			},
			TrustedProxies: []string{"127.0.0.1", "::1"},
			MaxSessions:    5,
			Captcha: CaptchaConfig{
				Provider:             "none",
				TriggerAfterFailures: 3,
			},
			BindSessionIP: true,
			BindSessionUA: true,
			TLS: TLSConfig{
				Enabled:      true,
				CertValidity: "8760h",
			},
		},
		Agent: AgentConfig{
			SocketPath:      "/run/nxpanel/agent.sock",
			Token:           "",
			ShutdownTimeout: "10s",
			ClientTimeout:   "30s",
			DialTimeout:     "3s",
			IdleConnTimeout: "30s",
			MaxReadSize:     "16M",
			MaxDownloadSize: "256M",
			DownloadTimeout: "2m",
		},
		Nginx: NginxConfig{
			Bin:                 "",
			ConfPath:            "",
			PanelDir:            "/opt/nxpanel/nginx",
			WebUser:             "",
			WebGroup:            "",
			Version:             "",
			IncludeInstalled:    false,
			LogDir:              "/www/wwwlogs",
			AllowedRootPrefixes: []string{"/www/wwwroot", "/var/www"},
			AllowedLogPrefixes:  []string{"/www/wwwlogs", "/var/log/nginx/nxpanel"},
			WebUserCandidates:   []string{"www-data", "nginx", "www", "nobody"},
			TestTimeout:         "15s",
			ReloadTimeout:       "15s",
			DumpTimeout:         "30s",
			DetectTimeout:       "10s",
			BackupMaxCount:      30,
			BackupMaxAge:        "168h",
		},
		ACME: ACMEConfig{
			UseStaging:    false,
			AutoRenewDays: 30,
			PreValidation: PreValidationConfig{
				DNSServer:     "",
				RetryInterval: "3s",
				RetryCount:    5,
			},
		},
		Database: DatabaseConfig{
			BusyTimeout: 5000,
		},
		Upgrade: UpgradeConfig{
			Enabled:       true,
			CheckInterval: "6h",
			GitHubRepo:    "luoye663/nxpanel",
		},
	}
}

// applyEnvOverrides 使用环境变量覆盖配置
// 每个字段单独检查，只覆盖非空的环境变量
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("NXPANEL_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("NXPANEL_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("NXPANEL_LOG_FILE"); v != "" {
		cfg.LogFile = v
	}
	if v := os.Getenv("NXPANEL_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("NXPANEL_SERVICE_LOGS_API_LOG_FILE"); v != "" {
		cfg.ServiceLogs.APILogFile = v
	}
	if v := os.Getenv("NXPANEL_SERVICE_LOGS_AGENT_LOG_FILE"); v != "" {
		cfg.ServiceLogs.AgentLogFile = v
	}
	if v := os.Getenv("NXPANEL_SERVICE_LOG_ROTATE_ENABLED"); v != "" {
		cfg.ServiceLogRotate.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("NXPANEL_SERVICE_LOG_ROTATE_INTERVAL"); v != "" {
		cfg.ServiceLogRotate.Interval = v
	}
	if v := os.Getenv("NXPANEL_SERVICE_LOG_ROTATE_MAX_SIZE"); v != "" {
		cfg.ServiceLogRotate.MaxSize = v
	}
	if v := os.Getenv("NXPANEL_SERVICE_LOG_ROTATE_MAX_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ServiceLogRotate.MaxCount = n
		}
	}
	if v := os.Getenv("NXPANEL_SERVICE_LOG_ROTATE_MAX_AGE"); v != "" {
		cfg.ServiceLogRotate.MaxAge = v
	}
	if v := os.Getenv("NXPANEL_API_LISTEN"); v != "" {
		cfg.API.Listen = v
	}
	if v := os.Getenv("NXPANEL_API_LOGIN_PATH"); v != "" {
		cfg.API.LoginPath = v
	}
	if v := os.Getenv("NXPANEL_API_PUBLIC_HEALTH"); v != "" {
		cfg.API.PublicHealth = v == "true" || v == "1"
	}
	if v := os.Getenv("NXPANEL_AGENT_SOCKET_PATH"); v != "" {
		cfg.Agent.SocketPath = v
	}
	if v := os.Getenv("NXPANEL_AGENT_SOCKET_GROUP"); v != "" {
		cfg.Agent.SocketGroup = v
	}
	if v := os.Getenv("NXPANEL_AGENT_TOKEN"); v != "" {
		cfg.Agent.Token = v
	}
	if v := os.Getenv("NXPANEL_AGENT_ALLOWED_ROOTS"); v != "" {
		cfg.Agent.AllowedRoots = strings.Split(v, ",")
	}
	if v := os.Getenv("NXPANEL_NGINX_BIN"); v != "" {
		cfg.Nginx.Bin = v
	}
	if v := os.Getenv("NXPANEL_NGINX_CONF_PATH"); v != "" {
		cfg.Nginx.ConfPath = v
	}
	if v := os.Getenv("NXPANEL_NGINX_PANEL_DIR"); v != "" {
		cfg.Nginx.PanelDir = v
	}
	if v := os.Getenv("NXPANEL_NGINX_WEB_USER"); v != "" {
		cfg.Nginx.WebUser = v
	}
	if v := os.Getenv("NXPANEL_NGINX_WEB_GROUP"); v != "" {
		cfg.Nginx.WebGroup = v
	}
	if v := os.Getenv("NXPANEL_ACME_USE_STAGING"); v != "" {
		cfg.ACME.UseStaging = v == "true" || v == "1"
	}
	if v := os.Getenv("NXPANEL_ACME_PRE_VALIDATION_DNS_SERVER"); v != "" {
		cfg.ACME.PreValidation.DNSServer = v
	}
	if v := os.Getenv("NXPANEL_ACME_PRE_VALIDATION_RETRY_INTERVAL"); v != "" {
		cfg.ACME.PreValidation.RetryInterval = v
	}
	if v := os.Getenv("NXPANEL_ACME_PRE_VALIDATION_RETRY_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ACME.PreValidation.RetryCount = n
		}
	}
	if v := os.Getenv("NXPANEL_API_SESSION_DURATION"); v != "" {
		cfg.API.SessionDuration = v
	}
	if v := os.Getenv("NXPANEL_API_READ_TIMEOUT"); v != "" {
		cfg.API.ReadTimeout = v
	}
	if v := os.Getenv("NXPANEL_API_WRITE_TIMEOUT"); v != "" {
		cfg.API.WriteTimeout = v
	}
	if v := os.Getenv("NXPANEL_API_IDLE_TIMEOUT"); v != "" {
		cfg.API.IdleTimeout = v
	}
	if v := os.Getenv("NXPANEL_API_SHUTDOWN_TIMEOUT"); v != "" {
		cfg.API.ShutdownTimeout = v
	}
	if v := os.Getenv("NXPANEL_API_SSE_HEARTBEAT"); v != "" {
		cfg.API.SSEHeartbeat = v
	}
	if v := os.Getenv("NXPANEL_API_SYSTEM_METRICS_INTERVAL"); v != "" {
		cfg.API.SystemMetricsInterval = v
	}
	if v := os.Getenv("NXPANEL_API_UPLOAD_TIMEOUT"); v != "" {
		cfg.API.UploadTimeout = v
	}
	if v := os.Getenv("NXPANEL_API_RATE_LIMIT_MAX_FAILURES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.API.RateLimit.MaxFailures = n
		}
	}
	if v := os.Getenv("NXPANEL_API_RATE_LIMIT_WINDOW"); v != "" {
		cfg.API.RateLimit.Window = v
	}
	if v := os.Getenv("NXPANEL_API_TRUSTED_PROXIES"); v != "" {
		cfg.API.TrustedProxies = strings.Split(v, ",")
	}
	if v := os.Getenv("NXPANEL_API_MAX_SESSIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.API.MaxSessions = n
		}
	}
	if v := os.Getenv("NXPANEL_API_CAPTCHA_PROVIDER"); v != "" {
		cfg.API.Captcha.Provider = v
	}
	if v := os.Getenv("NXPANEL_API_CAPTCHA_SITE_KEY"); v != "" {
		cfg.API.Captcha.SiteKey = v
	}
	if v := os.Getenv("NXPANEL_API_CAPTCHA_SECRET_KEY"); v != "" {
		cfg.API.Captcha.SecretKey = v
	}
	if v := os.Getenv("NXPANEL_API_CAPTCHA_TRIGGER_AFTER_FAILURES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.API.Captcha.TriggerAfterFailures = n
		}
	}
	if v := os.Getenv("NXPANEL_API_BIND_SESSION_IP"); v != "" {
		cfg.API.BindSessionIP = v == "true" || v == "1"
	}
	if v := os.Getenv("NXPANEL_API_BIND_SESSION_UA"); v != "" {
		cfg.API.BindSessionUA = v == "true" || v == "1"
	}
	if v := os.Getenv("NXPANEL_API_TLS_ENABLED"); v != "" {
		cfg.API.TLS.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("NXPANEL_API_TLS_CERT"); v != "" {
		cfg.API.TLS.Cert = v
	}
	if v := os.Getenv("NXPANEL_API_TLS_KEY"); v != "" {
		cfg.API.TLS.Key = v
	}
	if v := os.Getenv("NXPANEL_API_TLS_CERT_VALIDITY"); v != "" {
		cfg.API.TLS.CertValidity = v
	}
	if v := os.Getenv("NXPANEL_API_TLS_SANS"); v != "" {
		cfg.API.TLS.SANs = strings.Split(v, ",")
	}
	if v := os.Getenv("NXPANEL_AGENT_SHUTDOWN_TIMEOUT"); v != "" {
		cfg.Agent.ShutdownTimeout = v
	}
	if v := os.Getenv("NXPANEL_AGENT_CLIENT_TIMEOUT"); v != "" {
		cfg.Agent.ClientTimeout = v
	}
	if v := os.Getenv("NXPANEL_AGENT_DIAL_TIMEOUT"); v != "" {
		cfg.Agent.DialTimeout = v
	}
	if v := os.Getenv("NXPANEL_AGENT_IDLE_CONN_TIMEOUT"); v != "" {
		cfg.Agent.IdleConnTimeout = v
	}
	if v := os.Getenv("NXPANEL_AGENT_MAX_READ_SIZE"); v != "" {
		cfg.Agent.MaxReadSize = v
	}
	if v := os.Getenv("NXPANEL_AGENT_MAX_DOWNLOAD_SIZE"); v != "" {
		cfg.Agent.MaxDownloadSize = v
	}
	if v := os.Getenv("NXPANEL_AGENT_DOWNLOAD_TIMEOUT"); v != "" {
		cfg.Agent.DownloadTimeout = v
	}
	if v := os.Getenv("NXPANEL_NGINX_LOG_DIR"); v != "" {
		cfg.Nginx.LogDir = v
	}
	if v := os.Getenv("NXPANEL_NGINX_ALLOWED_ROOT_PREFIXES"); v != "" {
		cfg.Nginx.AllowedRootPrefixes = strings.Split(v, ",")
	}
	if v := os.Getenv("NXPANEL_NGINX_ALLOWED_LOG_PREFIXES"); v != "" {
		cfg.Nginx.AllowedLogPrefixes = strings.Split(v, ",")
	}
	if v := os.Getenv("NXPANEL_NGINX_WEB_USER_CANDIDATES"); v != "" {
		cfg.Nginx.WebUserCandidates = strings.Split(v, ",")
	}
	if v := os.Getenv("NXPANEL_NGINX_TEST_TIMEOUT"); v != "" {
		cfg.Nginx.TestTimeout = v
	}
	if v := os.Getenv("NXPANEL_NGINX_RELOAD_TIMEOUT"); v != "" {
		cfg.Nginx.ReloadTimeout = v
	}
	if v := os.Getenv("NXPANEL_NGINX_DUMP_TIMEOUT"); v != "" {
		cfg.Nginx.DumpTimeout = v
	}
	if v := os.Getenv("NXPANEL_NGINX_DETECT_TIMEOUT"); v != "" {
		cfg.Nginx.DetectTimeout = v
	}
	if v := os.Getenv("NXPANEL_NGINX_BACKUP_MAX_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Nginx.BackupMaxCount = n
		}
	}
	if v := os.Getenv("NXPANEL_NGINX_BACKUP_MAX_AGE"); v != "" {
		cfg.Nginx.BackupMaxAge = v
	}
	if v := os.Getenv("NXPANEL_DATABASE_BUSY_TIMEOUT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Database.BusyTimeout = n
		}
	}
	if v := os.Getenv("NXPANEL_UPGRADE_ENABLED"); v != "" {
		cfg.Upgrade.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("NXPANEL_UPGRADE_CHECK_INTERVAL"); v != "" {
		cfg.Upgrade.CheckInterval = v
	}
	if v := os.Getenv("NXPANEL_UPGRADE_GITHUB_REPO"); v != "" {
		cfg.Upgrade.GitHubRepo = v
	}
}

// ============================================================
// 日志初始化
// ============================================================

// InitLogger 初始化 slog 结构化日志
//
// 参数：
//   - level: 日志级别（debug/info/warn/error）
//   - format: 日志格式（json 或 text）
//
// 返回 slog.Logger 实例，调用方可用 slog.SetDefault() 设为全局默认
func InitLogger(level, format string) *slog.Logger {
	return InitLoggerToFile(level, format, "")
}

func InitLoggerToFile(level, format, logFile string) *slog.Logger {
	logger, _ := InitLoggerToFileWithRotation(level, format, logFile, ServiceLogRotateConfig{Enabled: false})
	return logger
}

func InitLoggerToFileWithRotation(level, format, logFile string, rotate ServiceLogRotateConfig) (*slog.Logger, io.Closer) {
	lvl := parseLogLevel(level)

	opts := &slog.HandlerOptions{
		Level: lvl,
	}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	if logFile == "" {
		return slog.New(handler), noopCloser{}
	}

	if err := os.MkdirAll(filepath.Dir(logFile), 0755); err != nil {
		slog.Warn("创建日志目录失败，仅使用 stdout", "path", logFile, "error", err)
		return slog.New(handler), noopCloser{}
	}

	var writer io.Writer
	var closer io.Closer
	if rotate.Enabled {
		// 运行日志轮转在进程内重新打开文件，避免外部 rename 后继续写旧句柄。
		rotatingWriter, err := NewRotatingFileWriter(logFile, rotate)
		if err != nil {
			slog.Warn("打开轮转日志文件失败，仅使用 stdout", "path", logFile, "error", err)
			return slog.New(handler), noopCloser{}
		}
		writer = rotatingWriter
		closer = rotatingWriter
	} else {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			slog.Warn("打开日志文件失败，仅使用 stdout", "path", logFile, "error", err)
			return slog.New(handler), noopCloser{}
		}
		writer = f
		closer = f
	}

	var fileHandler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		fileHandler = slog.NewTextHandler(writer, opts)
	default:
		fileHandler = slog.NewJSONHandler(writer, opts)
	}

	return slog.New(&multiHandler{handlers: []slog.Handler{handler, fileHandler}}), closer
}

type noopCloser struct{}

func (noopCloser) Close() error { return nil }

type multiHandler struct {
	handlers []slog.Handler
}

func (m *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if err := h.Handle(ctx, r.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (m *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (m *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

// parseLogLevel 将字符串转换为 slog.Level
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ============================================================
// 辅助方法
// ============================================================

var Version = "0.1.0-dev"

var Now = func() time.Time {
	return time.Now().UTC()
}

// ParseDurationOrDefault 解析时长字符串，失败时返回默认值
func ParseDurationOrDefault(s string, defaultVal time.Duration) time.Duration {
	if s == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

// WriteBack 将当前配置写回配置文件，保留注释和格式
// 只更新指定的字段，其他内容不变
func (c *Config) WriteBack() error {
	if c.ConfigFile == "" {
		return fmt.Errorf("配置文件路径未设置")
	}

	data, err := os.ReadFile(c.ConfigFile)
	if err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	setYAMLNodeValue(&root, "nginx", "bin", c.Nginx.Bin)
	setYAMLNodeValue(&root, "nginx", "conf_path", c.Nginx.ConfPath)
	setYAMLNodeValue(&root, "nginx", "web_user", c.Nginx.WebUser)
	setYAMLNodeValue(&root, "nginx", "web_group", c.Nginx.WebGroup)
	setYAMLNodeValue(&root, "nginx", "version", c.Nginx.Version)
	setYAMLNodeBool(&root, "nginx", "include_installed", c.Nginx.IncludeInstalled)
	deleteYAMLNode(&root, "acme", "auto_renew_days")
	deleteYAMLNode(&root, "acme", "scheduler_interval")

	// API 安全配置
	setYAMLNodeValue(&root, "api", "login_path", c.API.LoginPath)
	setYAMLNodeBool(&root, "api", "public_health", c.API.PublicHealth)
	setYAMLNodeScalar3L(&root, "api", "rate_limit", "max_failures", strconv.Itoa(c.API.RateLimit.MaxFailures), "!!int")
	setYAMLNodeValue3L(&root, "api", "rate_limit", "window", c.API.RateLimit.Window)
	setYAMLNodeScalar(&root, "api", "max_sessions", strconv.Itoa(c.API.MaxSessions), "!!int")
	setYAMLNodeBool(&root, "api", "bind_session_ip", c.API.BindSessionIP)
	setYAMLNodeBool(&root, "api", "bind_session_ua", c.API.BindSessionUA)
	setYAMLNodeSequence(&root, "api", "trusted_proxies", c.API.TrustedProxies)
	setYAMLNodeValue3L(&root, "api", "captcha", "provider", c.API.Captcha.Provider)
	setYAMLNodeValue3L(&root, "api", "captcha", "site_key", c.API.Captcha.SiteKey)
	setYAMLNodeValue3L(&root, "api", "captcha", "secret_key", c.API.Captcha.SecretKey)
	setYAMLNodeScalar3L(&root, "api", "captcha", "trigger_after_failures", strconv.Itoa(c.API.Captcha.TriggerAfterFailures), "!!int")

	// API TLS 配置
	setYAMLNodeBool3L(&root, "api", "tls", "enabled", c.API.TLS.Enabled)
	setYAMLNodeValue3L(&root, "api", "tls", "cert", c.API.TLS.Cert)
	setYAMLNodeValue3L(&root, "api", "tls", "key", c.API.TLS.Key)
	setYAMLNodeValue3L(&root, "api", "tls", "cert_validity", c.API.TLS.CertValidity)
	deleteYAMLNode3L(&root, "api", "tls", "http_redirect")

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	return os.WriteFile(c.ConfigFile, buf.Bytes(), 0600)
}

func normalizeLoginPath(cfg *Config) {
	if err := ValidateLoginPath(cfg.API.LoginPath); err == nil {
		return
	}
	for i := 0; i < 3; i++ {
		path := GenerateLoginPath()
		if ValidateLoginPath(path) == nil {
			cfg.API.LoginPath = path
			break
		}
	}
	if cfg.API.LoginPath == "" {
		cfg.API.LoginPath = "/panelfallback"
	}
	// 随机入口尽力写回配置文件，避免重启后管理员丢失入口；失败仅记录告警继续启动。
	if cfg.ConfigFile != "" {
		if err := cfg.WriteBack(); err != nil {
			slog.Warn("写回随机登录入口失败，请从启动日志记录入口", "login_path", cfg.API.LoginPath, "error", err)
		} else {
			slog.Info("已生成并写回随机登录入口", "login_path", cfg.API.LoginPath)
		}
	}
}

func (c *Config) ReloadFromDisk() error {
	if c.ConfigFile == "" {
		return fmt.Errorf("配置文件路径未设置")
	}
	fresh, err := LoadConfig(c.ConfigFile)
	if err != nil {
		return err
	}
	c.Nginx.Bin = fresh.Nginx.Bin
	c.Nginx.ConfPath = fresh.Nginx.ConfPath
	c.Nginx.Version = fresh.Nginx.Version
	c.Nginx.IncludeInstalled = fresh.Nginx.IncludeInstalled
	c.Nginx.WebUser = fresh.Nginx.WebUser
	c.Nginx.WebGroup = fresh.Nginx.WebGroup
	return nil
}

// setYAMLNodeValue 在 yaml.Node 树中找到 section.key 并更新其值
func setYAMLNodeValue(root *yaml.Node, section, key, value string) {
	setYAMLNodeScalar(root, section, key, value, "!!str")
}

// setYAMLNodeBool 在 yaml.Node 树中找到 section.key 并更新布尔值
func setYAMLNodeBool(root *yaml.Node, section, key string, value bool) {
	s := "false"
	if value {
		s = "true"
	}
	setYAMLNodeScalar(root, section, key, s, "!!bool")
}

// setYAMLNodeScalar 在 yaml.Node 树中找到 section.key 并更新其值和标签
func setYAMLNodeScalar(root *yaml.Node, section, key, value, tag string) {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == section && root.Content[i+1].Kind == yaml.MappingNode {
			secNode := root.Content[i+1]
			for j := 0; j+1 < len(secNode.Content); j += 2 {
				if secNode.Content[j].Value == key {
					secNode.Content[j+1].Value = value
					secNode.Content[j+1].Tag = tag
					return
				}
			}
			// key 不存在则追加
			secNode.Content = append(secNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
				&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: tag},
			)
			return
		}
	}
}

func deleteYAMLNode(root *yaml.Node, section, key string) {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != section || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		secNode := root.Content[i+1]
		for j := 0; j+1 < len(secNode.Content); j += 2 {
			if secNode.Content[j].Value == key {
				secNode.Content = append(secNode.Content[:j], secNode.Content[j+2:]...)
				return
			}
		}
		return
	}
}

// setYAMLNodeScalar3L 在 yaml.Node 树中找到 section.subsection.key 并更新其值和标签
func setYAMLNodeScalar3L(root *yaml.Node, section, subsection, key, value, tag string) {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == section && root.Content[i+1].Kind == yaml.MappingNode {
			secNode := root.Content[i+1]
			for j := 0; j+1 < len(secNode.Content); j += 2 {
				if secNode.Content[j].Value == subsection && secNode.Content[j+1].Kind == yaml.MappingNode {
					subNode := secNode.Content[j+1]
					for k := 0; k+1 < len(subNode.Content); k += 2 {
						if subNode.Content[k].Value == key {
							subNode.Content[k+1].Value = value
							subNode.Content[k+1].Tag = tag
							return
						}
					}
					// key 不存在则追加
					subNode.Content = append(subNode.Content,
						&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
						&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: tag},
					)
					return
				}
			}
			// subsection 不存在则追加整个子映射
			secNode.Content = append(secNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: subsection, Tag: "!!str"},
				&yaml.Node{
					Kind: yaml.MappingNode,
					Tag:  "!!map",
					Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
						{Kind: yaml.ScalarNode, Value: value, Tag: tag},
					},
				},
			)
			return
		}
	}
}

// setYAMLNodeValue3L 在 yaml.Node 树中找到 section.subsection.key 并更新字符串值
func setYAMLNodeValue3L(root *yaml.Node, section, subsection, key, value string) {
	setYAMLNodeScalar3L(root, section, subsection, key, value, "!!str")
}

// setYAMLNodeBool3L 在 yaml.Node 树中找到 section.subsection.key 并更新布尔值
func setYAMLNodeBool3L(root *yaml.Node, section, subsection, key string, value bool) {
	s := "false"
	if value {
		s = "true"
	}
	setYAMLNodeScalar3L(root, section, subsection, key, s, "!!bool")
}

func deleteYAMLNode3L(root *yaml.Node, section, subsection, key string) {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != section || root.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		secNode := root.Content[i+1]
		for j := 0; j+1 < len(secNode.Content); j += 2 {
			if secNode.Content[j].Value != subsection || secNode.Content[j+1].Kind != yaml.MappingNode {
				continue
			}
			subNode := secNode.Content[j+1]
			for k := 0; k+1 < len(subNode.Content); k += 2 {
				if subNode.Content[k].Value == key {
					subNode.Content = append(subNode.Content[:k], subNode.Content[k+2:]...)
					return
				}
			}
			return
		}
		return
	}
}

// setYAMLNodeSequence 在 yaml.Node 树中找到 section.key 并更新字符串列表
func setYAMLNodeSequence(root *yaml.Node, section, key string, values []string) {
	if root.Kind == yaml.DocumentNode && len(root.Content) > 0 {
		root = root.Content[0]
	}
	if root.Kind != yaml.MappingNode {
		return
	}
	seqNode := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range values {
		seqNode.Content = append(seqNode.Content, &yaml.Node{
			Kind:  yaml.ScalarNode,
			Value: v,
			Tag:   "!!str",
		})
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == section && root.Content[i+1].Kind == yaml.MappingNode {
			secNode := root.Content[i+1]
			for j := 0; j+1 < len(secNode.Content); j += 2 {
				if secNode.Content[j].Value == key {
					secNode.Content[j+1] = seqNode
					return
				}
			}
			// key 不存在则追加
			secNode.Content = append(secNode.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Value: key, Tag: "!!str"},
				seqNode,
			)
			return
		}
	}
}

func ParseSizeOrDefault(s string, defaultVal int64) int64 {
	if s == "" {
		return defaultVal
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "G"):
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "M"):
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	case strings.HasSuffix(s, "K"):
		multiplier = 1024
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return defaultVal
	}
	return n * multiplier
}

func (c *Config) ServiceLogDir() string {
	return filepath.Join(c.DataDir, "logs")
}

func (c *Config) APILogPath() string {
	if c.ServiceLogs.APILogFile != "" {
		return c.ServiceLogs.APILogFile
	}
	if c.LogFile != "" {
		return c.LogFile
	}
	return filepath.Join(c.DataDir, "logs", "api.log")
}

func (c *Config) AgentLogPath() string {
	if c.ServiceLogs.AgentLogFile != "" {
		return c.ServiceLogs.AgentLogFile
	}
	if c.LogFile != "" {
		return c.LogFile
	}
	return filepath.Join(c.DataDir, "logs", "agent.log")
}

func (c *Config) TaskLogDir() string {
	return filepath.Join(c.DataDir, "logs", "tasks")
}
