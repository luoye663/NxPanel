// app 包的配置加载测试
package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultConfig 验证默认配置值的合理性
func TestDefaultConfig(t *testing.T) {
	cfg := defaultConfig()

	// 默认日志级别为 info
	if cfg.LogLevel != "info" {
		t.Errorf("默认 LogLevel 期望 info，实际 %s", cfg.LogLevel)
	}

	// 默认日志格式为 json
	if cfg.LogFormat != "json" {
		t.Errorf("默认 LogFormat 期望 json，实际 %s", cfg.LogFormat)
	}

	// 默认 API 监听地址
	if cfg.API.Listen != "127.0.0.1:8888" {
		t.Errorf("默认 API.Listen 期望 127.0.0.1:8888，实际 %s", cfg.API.Listen)
	}

	// 默认 Agent Socket 路径
	if cfg.Agent.SocketPath != "/run/nxpanel/agent.sock" {
		t.Errorf("默认 Agent.SocketPath 不正确: %s", cfg.Agent.SocketPath)
	}
	if cfg.Agent.MaxReadSize != "16M" {
		t.Errorf("默认 Agent.MaxReadSize 期望 16M，实际 %s", cfg.Agent.MaxReadSize)
	}
	if cfg.Agent.MaxDownloadSize != "256M" {
		t.Errorf("默认 Agent.MaxDownloadSize 期望 256M，实际 %s", cfg.Agent.MaxDownloadSize)
	}
	if cfg.Agent.DownloadTimeout != "2m" {
		t.Errorf("默认 Agent.DownloadTimeout 期望 2m，实际 %s", cfg.Agent.DownloadTimeout)
	}

	// 默认 Nginx 主配置路径
	if cfg.Nginx.ConfPath != "" {
		t.Errorf("默认 Nginx.ConfPath 应为空（由 detect 自动检测），实际 %s", cfg.Nginx.ConfPath)
	}

	if cfg.Nginx.Version != "" {
		t.Errorf("默认 Nginx.Version 应为空，实际 %s", cfg.Nginx.Version)
	}
	if cfg.Nginx.IncludeInstalled {
		t.Error("默认 Nginx.IncludeInstalled 应为 false")
	}

	// 默认面板配置目录
	if cfg.Nginx.PanelDir != "/opt/nxpanel/nginx" {
		t.Errorf("默认 Nginx.PanelDir 不正确: %s", cfg.Nginx.PanelDir)
	}
}

// TestLoadConfig_FileNotExist 配置文件不存在时应该返回默认值
func TestLoadConfig_FileNotExist(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/config.yaml")
	if err != nil {
		t.Fatalf("文件不存在时应返回默认配置，不应报错: %v", err)
	}

	if cfg.LogLevel != "info" {
		t.Errorf("文件不存在时应使用默认 LogLevel=info，实际 %s", cfg.LogLevel)
	}
}

// TestLoadConfig_ValidYAML 验证正常 YAML 文件的解析
func TestLoadConfig_ValidYAML(t *testing.T) {
	// 创建临时配置文件
	content := []byte(`
log_level: debug
log_format: text
data_dir: /tmp/test-data

api:
  listen: "0.0.0.0:9999"

agent:
  socket_path: /tmp/test.sock
  token: "test-secret-token"
  max_read_size: "8M"
  max_download_size: "128M"
  download_timeout: "90s"

nginx:
  bin: /usr/local/bin/nginx
  conf_path: /etc/nginx/nginx.conf
  panel_dir: /tmp/panel-nginx
`)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 验证 YAML 值被正确解析
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel 期望 debug，实际 %s", cfg.LogLevel)
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat 期望 text，实际 %s", cfg.LogFormat)
	}
	if cfg.DataDir != "/tmp/test-data" {
		t.Errorf("DataDir 期望 /tmp/test-data，实际 %s", cfg.DataDir)
	}
	if cfg.API.Listen != "0.0.0.0:9999" {
		t.Errorf("API.Listen 期望 0.0.0.0:9999，实际 %s", cfg.API.Listen)
	}
	if cfg.Agent.SocketPath != "/tmp/test.sock" {
		t.Errorf("Agent.SocketPath 期望 /tmp/test.sock，实际 %s", cfg.Agent.SocketPath)
	}
	if cfg.Agent.MaxReadSize != "8M" {
		t.Errorf("Agent.MaxReadSize 期望 8M，实际 %s", cfg.Agent.MaxReadSize)
	}
	if cfg.Agent.MaxDownloadSize != "128M" {
		t.Errorf("Agent.MaxDownloadSize 期望 128M，实际 %s", cfg.Agent.MaxDownloadSize)
	}
	if cfg.Agent.DownloadTimeout != "90s" {
		t.Errorf("Agent.DownloadTimeout 期望 90s，实际 %s", cfg.Agent.DownloadTimeout)
	}
	if cfg.Agent.Token != "test-secret-token" {
		t.Errorf("Agent.Token 不正确")
	}
	if cfg.Nginx.Bin != "/usr/local/bin/nginx" {
		t.Errorf("Nginx.Bin 不正确: %s", cfg.Nginx.Bin)
	}
}

// TestLoadConfig_EnvOverride 验证环境变量可以覆盖 YAML 配置
func TestLoadConfig_EnvOverride(t *testing.T) {
	// 创建临时配置文件（有值）
	content := []byte(`
log_level: info
api:
  listen: "127.0.0.1:8888"
agent:
  socket_path: /run/default.sock
  token: "yaml-token"
`)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	// 设置环境变量
	t.Setenv("NXPANEL_LOG_LEVEL", "error")
	t.Setenv("NXPANEL_API_LISTEN", "0.0.0.0:3000")
	t.Setenv("NXPANEL_AGENT_TOKEN", "env-override-token")
	t.Setenv("NXPANEL_AGENT_MAX_READ_SIZE", "12M")
	t.Setenv("NXPANEL_AGENT_MAX_DOWNLOAD_SIZE", "512M")
	t.Setenv("NXPANEL_AGENT_DOWNLOAD_TIMEOUT", "3m")

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 验证环境变量覆盖了 YAML 值
	if cfg.LogLevel != "error" {
		t.Errorf("环境变量覆盖后 LogLevel 期望 error，实际 %s", cfg.LogLevel)
	}
	if cfg.API.Listen != "0.0.0.0:3000" {
		t.Errorf("环境变量覆盖后 API.Listen 期望 0.0.0.0:3000，实际 %s", cfg.API.Listen)
	}
	if cfg.Agent.Token != "env-override-token" {
		t.Errorf("环境变量覆盖后 Agent.Token 不正确")
	}
	if cfg.Agent.MaxReadSize != "12M" {
		t.Errorf("环境变量覆盖后 Agent.MaxReadSize 期望 12M，实际 %s", cfg.Agent.MaxReadSize)
	}
	if cfg.Agent.MaxDownloadSize != "512M" {
		t.Errorf("环境变量覆盖后 Agent.MaxDownloadSize 期望 512M，实际 %s", cfg.Agent.MaxDownloadSize)
	}
	if cfg.Agent.DownloadTimeout != "3m" {
		t.Errorf("环境变量覆盖后 Agent.DownloadTimeout 期望 3m，实际 %s", cfg.Agent.DownloadTimeout)
	}
	// 未被环境变量覆盖的字段应保持 YAML 中的值
	if cfg.Agent.SocketPath != "/run/default.sock" {
		t.Errorf("未被覆盖的 Agent.SocketPath 应保持 YAML 值")
	}
}

// TestLoadConfig_InvalidYAML 验证无效 YAML 返回错误
func TestLoadConfig_InvalidYAML(t *testing.T) {
	content := []byte(`
log_level: [invalid
  broken yaml
`)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	_, err := LoadConfig(tmpFile)
	if err == nil {
		t.Error("无效 YAML 应返回错误")
	}
}

// TestLoadConfig_PartialYAML 验证部分 YAML 字段使用默认值
func TestLoadConfig_PartialYAML(t *testing.T) {
	// 只设置部分字段，其他应使用默认值
	content := []byte(`
api:
  listen: "0.0.0.0:8080"
`)

	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}

	// 设置了 api.listen
	if cfg.API.Listen != "0.0.0.0:8080" {
		t.Errorf("API.Listen 期望 0.0.0.0:8080，实际 %s", cfg.API.Listen)
	}
	// 未设置的字段应使用默认值
	if cfg.LogLevel != "info" {
		t.Errorf("未设置的 LogLevel 应使用默认值 info，实际 %s", cfg.LogLevel)
	}
}

func TestServiceLogPathResolution(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := defaultConfig()
	cfg.DataDir = tmpDir

	if got := cfg.APILogPath(); got != filepath.Join(tmpDir, "logs", "api.log") {
		t.Fatalf("API 默认日志路径不正确: %s", got)
	}
	if got := cfg.AgentLogPath(); got != filepath.Join(tmpDir, "logs", "agent.log") {
		t.Fatalf("Agent 默认日志路径不正确: %s", got)
	}

	cfg.LogFile = filepath.Join(tmpDir, "logs", "shared.log")
	if got := cfg.APILogPath(); got != cfg.LogFile {
		t.Fatalf("兼容顶层 log_file 时 API 路径不正确: %s", got)
	}
	if got := cfg.AgentLogPath(); got != cfg.LogFile {
		t.Fatalf("兼容顶层 log_file 时 Agent 路径不正确: %s", got)
	}

	cfg.ServiceLogs.APILogFile = filepath.Join(tmpDir, "api-custom.log")
	cfg.ServiceLogs.AgentLogFile = filepath.Join(tmpDir, "agent-custom.log")
	if got := cfg.APILogPath(); got != cfg.ServiceLogs.APILogFile {
		t.Fatalf("service_logs.api_log_file 优先级不正确: %s", got)
	}
	if got := cfg.AgentLogPath(); got != cfg.ServiceLogs.AgentLogFile {
		t.Fatalf("service_logs.agent_log_file 优先级不正确: %s", got)
	}
}

func TestWriteBack_NullToValue(t *testing.T) {
	content := []byte(`nginx:
  bin: /usr/bin/openresty
  conf_path:
  panel_dir: /opt/nxpanel/nginx
`)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	cfg.ConfigFile = tmpFile
	cfg.Nginx.ConfPath = "/usr/local/openresty/nginx/conf/nginx.conf"

	if err := cfg.WriteBack(); err != nil {
		t.Fatalf("WriteBack 失败: %v", err)
	}

	written, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("读取写回文件失败: %v", err)
	}

	got := string(written)
	if strings.Contains(got, "!!null") {
		t.Errorf("写回内容不应包含 !!null，实际:\n%s", got)
	}
	if !strings.Contains(got, "/usr/local/openresty/nginx/conf/nginx.conf") {
		t.Errorf("写回内容应包含正确的 conf_path，实际:\n%s", got)
	}
}

func TestWriteBack_BoolValue(t *testing.T) {
	content := []byte(`nginx:
  bin: /usr/bin/openresty
  conf_path: /etc/nginx/nginx.conf
  include_installed: false
`)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	cfg.ConfigFile = tmpFile
	cfg.Nginx.IncludeInstalled = true

	if err := cfg.WriteBack(); err != nil {
		t.Fatalf("WriteBack 失败: %v", err)
	}

	written, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("读取写回文件失败: %v", err)
	}

	got := string(written)
	if strings.Contains(got, `"true"`) {
		t.Errorf("include_installed 不应写为带引号的字符串，实际:\n%s", got)
	}
	if !strings.Contains(got, "include_installed: true") {
		t.Errorf("include_installed 应写为布尔值 true，实际:\n%s", got)
	}
}

func TestWriteBack_BoolValue_False(t *testing.T) {
	content := []byte(`nginx:
  bin: /usr/bin/openresty
  conf_path: /etc/nginx/nginx.conf
  include_installed: true
`)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	cfg.ConfigFile = tmpFile
	cfg.Nginx.IncludeInstalled = false

	if err := cfg.WriteBack(); err != nil {
		t.Fatalf("WriteBack 失败: %v", err)
	}

	written, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("读取写回文件失败: %v", err)
	}

	got := string(written)
	if strings.Contains(got, `"false"`) {
		t.Errorf("include_installed 不应写为带引号的字符串，实际:\n%s", got)
	}
	if !strings.Contains(got, "include_installed: false") {
		t.Errorf("include_installed 应写为布尔值 false，实际:\n%s", got)
	}
}

func TestWriteBack_RemovesLegacyHTTPRedirect(t *testing.T) {
	content := []byte(`api:
  tls:
    enabled: true
    cert: ""
    key: ""
    cert_validity: "8760h"
    http_redirect: "18888"
`)
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatalf("创建临时配置文件失败: %v", err)
	}

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	cfg.ConfigFile = tmpFile

	if err := cfg.WriteBack(); err != nil {
		t.Fatalf("WriteBack 失败: %v", err)
	}

	written, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatalf("读取写回文件失败: %v", err)
	}

	got := string(written)
	if strings.Contains(got, "http_redirect") {
		t.Errorf("写回内容不应包含遗留 http_redirect，实际:\n%s", got)
	}
}

func TestReloadFromDisk(t *testing.T) {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "config.yaml")

	initial := []byte(`nginx:
  bin: /usr/bin/openresty
  conf_path:
  version: ""
`)
	os.WriteFile(tmpFile, initial, 0644)

	cfg, err := LoadConfig(tmpFile)
	if err != nil {
		t.Fatalf("加载配置失败: %v", err)
	}
	cfg.ConfigFile = tmpFile

	if cfg.Nginx.ConfPath != "" {
		t.Fatalf("初始 ConfPath 应为空")
	}

	updated := []byte(`nginx:
  bin: /usr/bin/openresty
  conf_path: /usr/local/openresty/nginx/conf/nginx.conf
  version: openresty/1.25.3.1
  include_installed: true
`)
	os.WriteFile(tmpFile, updated, 0644)

	if err := cfg.ReloadFromDisk(); err != nil {
		t.Fatalf("ReloadFromDisk 失败: %v", err)
	}

	if cfg.Nginx.ConfPath != "/usr/local/openresty/nginx/conf/nginx.conf" {
		t.Errorf("ConfPath = %q, want /usr/local/openresty/nginx/conf/nginx.conf", cfg.Nginx.ConfPath)
	}
	if cfg.Nginx.Version != "openresty/1.25.3.1" {
		t.Errorf("Version = %q, want openresty/1.25.3.1", cfg.Nginx.Version)
	}
	if !cfg.Nginx.IncludeInstalled {
		t.Error("IncludeInstalled 应为 true")
	}
}
