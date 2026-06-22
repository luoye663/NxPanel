// app 包的日志初始化测试
package app

import (
	"log/slog"
	"testing"
)

// TestInitLogger_JSONFormat 验证 JSON 格式日志初始化
func TestInitLogger_JSONFormat(t *testing.T) {
	logger := InitLogger("info", "json")
	if logger == nil {
		t.Fatal("InitLogger 不应返回 nil")
	}
}

// TestInitLogger_TextFormat 验证 text 格式日志初始化
func TestInitLogger_TextFormat(t *testing.T) {
	logger := InitLogger("info", "text")
	if logger == nil {
		t.Fatal("InitLogger 不应返回 nil")
	}
}

// TestParseLogLevel 验证日志级别解析
func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"Debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo}, // 未知级别降级为 info
		{"", slog.LevelInfo},        // 空字符串降级为 info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseLogLevel(tt.input)
			if result != tt.expected {
				t.Errorf("parseLogLevel(%q) = %v, 期望 %v", tt.input, result, tt.expected)
			}
		})
	}
}
