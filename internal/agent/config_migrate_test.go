package agent

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestMigrateConfig_AddMissingFields 测试添加缺失字段
func TestMigrateConfig_AddMissingFields(t *testing.T) {
	// 创建临时目录
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 写入一个只有部分字段的配置
	initialConfig := `
log_level: debug
api:
  listen: "127.0.0.1:9999"
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	// 执行迁移
	if err := MigrateConfig(configPath); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 读取迁移后的配置
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后配置失败: %v", err)
	}

	// 解析配置
	var cfg struct {
		LogLevel string `yaml:"log_level"`
		DataDir  string `yaml:"data_dir"`
		API      struct {
			Listen          string `yaml:"listen"`
			SessionDuration string `yaml:"session_duration"`
		} `yaml:"api"`
		Agent struct {
			SocketPath string `yaml:"socket_path"`
		} `yaml:"agent"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("解析迁移后配置失败: %v", err)
	}

	// 验证用户修改的值被保留
	if cfg.LogLevel != "debug" {
		t.Errorf("log_level 应为 debug，实际为 %s", cfg.LogLevel)
	}
	if cfg.API.Listen != "127.0.0.1:9999" {
		t.Errorf("api.listen 应为 127.0.0.1:9999，实际为 %s", cfg.API.Listen)
	}

	// 验证缺失字段被添加
	if cfg.DataDir == "" {
		t.Error("data_dir 应该被添加")
	}
	if cfg.API.SessionDuration == "" {
		t.Error("api.session_duration 应该被添加")
	}
	if cfg.Agent.SocketPath == "" {
		t.Error("agent.socket_path 应该被添加")
	}
}

// TestMigrateConfig_PreserveUserValues 测试保留用户修改的值
func TestMigrateConfig_PreserveUserValues(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 用户自定义的配置
	initialConfig := `
log_level: warn
api:
  listen: "0.0.0.0:8080"
  session_duration: 48h
nginx:
  log_dir: /custom/log/dir
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	// 执行迁移
	if err := MigrateConfig(configPath); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 读取迁移后的配置
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后配置失败: %v", err)
	}

	var cfg struct {
		LogLevel string `yaml:"log_level"`
		API      struct {
			Listen          string `yaml:"listen"`
			SessionDuration string `yaml:"session_duration"`
		} `yaml:"api"`
		Nginx struct {
			LogDir string `yaml:"log_dir"`
		} `yaml:"nginx"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("解析迁移后配置失败: %v", err)
	}

	// 验证所有用户值都被保留
	if cfg.LogLevel != "warn" {
		t.Errorf("log_level 应为 warn，实际为 %s", cfg.LogLevel)
	}
	if cfg.API.Listen != "0.0.0.0:8080" {
		t.Errorf("api.listen 应为 0.0.0.0:8080，实际为 %s", cfg.API.Listen)
	}
	if cfg.API.SessionDuration != "48h" {
		t.Errorf("api.session_duration 应为 48h，实际为 %s", cfg.API.SessionDuration)
	}
	if cfg.Nginx.LogDir != "/custom/log/dir" {
		t.Errorf("nginx.log_dir 应为 /custom/log/dir，实际为 %s", cfg.Nginx.LogDir)
	}
}

// TestMigrateConfig_EmptyConfig 测试空配置文件
func TestMigrateConfig_EmptyConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 空配置（空映射）
	initialConfig := `log_level: info
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	// 执行迁移
	if err := MigrateConfig(configPath); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 读取迁移后的配置
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后配置失败: %v", err)
	}

	var cfg struct {
		API struct {
			Listen string `yaml:"listen"`
		} `yaml:"api"`
		Agent struct {
			SocketPath string `yaml:"socket_path"`
		} `yaml:"agent"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("解析迁移后配置失败: %v", err)
	}

	// 验证默认值被添加
	if cfg.API.Listen == "" {
		t.Error("api.listen 应该有默认值")
	}
	if cfg.Agent.SocketPath == "" {
		t.Error("agent.socket_path 应该有默认值")
	}
}

// TestMigrateConfig_InvalidYAML 测试无效的 YAML 文件
func TestMigrateConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 无效的 YAML
	initialConfig := `
log_level: info
  invalid_indent: bad
    another_bad: true
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	// 执行迁移，应该失败
	err := MigrateConfig(configPath)
	if err == nil {
		t.Error("迁移应该失败，但成功了")
	}
}

// TestMigrateConfig_FileNotExists 测试配置文件不存在
func TestMigrateConfig_FileNotExists(t *testing.T) {
	configPath := "/tmp/nonexistent_config.yaml"

	// 执行迁移，应该失败
	err := MigrateConfig(configPath)
	if err == nil {
		t.Error("迁移应该失败，但成功了")
	}
}

// TestMigrateConfig_NestedFields 测试嵌套字段的迁移
func TestMigrateConfig_NestedFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// 只有部分嵌套字段
	initialConfig := `
api:
  listen: "127.0.0.1:8888"
  rate_limit:
    max_failures: 10
nginx:
  bin: /usr/sbin/nginx
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("写入初始配置失败: %v", err)
	}

	// 执行迁移
	if err := MigrateConfig(configPath); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	// 读取迁移后的配置
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("读取迁移后配置失败: %v", err)
	}

	var cfg struct {
		API struct {
			Listen    string `yaml:"listen"`
			RateLimit struct {
				MaxFailures int    `yaml:"max_failures"`
				Window      string `yaml:"window"`
			} `yaml:"rate_limit"`
		} `yaml:"api"`
		Nginx struct {
			Bin            string `yaml:"bin"`
			LogDir         string `yaml:"log_dir"`
			BackupMaxCount int    `yaml:"backup_max_count"`
		} `yaml:"nginx"`
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("解析迁移后配置失败: %v", err)
	}

	// 验证用户值保留
	if cfg.API.RateLimit.MaxFailures != 10 {
		t.Errorf("api.rate_limit.max_failures 应为 10，实际为 %d", cfg.API.RateLimit.MaxFailures)
	}
	if cfg.Nginx.Bin != "/usr/sbin/nginx" {
		t.Errorf("nginx.bin 应为 /usr/sbin/nginx，实际为 %s", cfg.Nginx.Bin)
	}

	// 验证缺失字段添加
	if cfg.API.RateLimit.Window == "" {
		t.Error("api.rate_limit.window 应该被添加")
	}
	if cfg.Nginx.LogDir == "" {
		t.Error("nginx.log_dir 应该被添加")
	}
	if cfg.Nginx.BackupMaxCount == 0 {
		t.Error("nginx.backup_max_count 应该被添加")
	}
}

// TestDeepCopyNode 测试深拷贝
func TestDeepCopyNode(t *testing.T) {
	original := &yaml.Node{
		Kind: yaml.MappingNode,
		Tag:  "!!map",
		Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "key1"},
			{Kind: yaml.ScalarNode, Value: "value1"},
			{Kind: yaml.ScalarNode, Value: "key2"},
			{
				Kind: yaml.SequenceNode,
				Content: []*yaml.Node{
					{Kind: yaml.ScalarNode, Value: "item1"},
					{Kind: yaml.ScalarNode, Value: "item2"},
				},
			},
		},
	}

	copied := deepCopyNode(original)

	// 验证拷贝
	if copied.Kind != original.Kind {
		t.Error("Kind 不匹配")
	}
	if len(copied.Content) != len(original.Content) {
		t.Error("Content 长度不匹配")
	}

	// 修改拷贝不应影响原始
	copied.Content[0].Value = "modified"
	if original.Content[0].Value == "modified" {
		t.Error("修改拷贝影响了原始节点")
	}
}

// TestIsLeafNode 测试叶子节点判断
func TestIsLeafNode(t *testing.T) {
	tests := []struct {
		node   *yaml.Node
		expect bool
	}{
		{&yaml.Node{Kind: yaml.ScalarNode}, true},
		{&yaml.Node{Kind: yaml.MappingNode}, false},
		{&yaml.Node{Kind: yaml.SequenceNode}, false},
		{nil, false},
	}

	for _, tt := range tests {
		if got := isLeafNode(tt.node); got != tt.expect {
			t.Errorf("isLeafNode(%v) = %v, want %v", tt.node, got, tt.expect)
		}
	}
}

// TestGetNodeValue 测试获取节点值
func TestGetNodeValue(t *testing.T) {
	tests := []struct {
		node   *yaml.Node
		expect interface{}
	}{
		{&yaml.Node{Kind: yaml.ScalarNode, Value: "hello"}, "hello"},
		{&yaml.Node{Kind: yaml.MappingNode}, "{...}"},
		{&yaml.Node{Kind: yaml.SequenceNode}, "[...]"},
		{nil, nil},
	}

	for _, tt := range tests {
		got := getNodeValue(tt.node)
		if got != tt.expect {
			t.Errorf("getNodeValue(%v) = %v, want %v", tt.node, got, tt.expect)
		}
	}
}
