// agent 包测试 — 日志 handler 和 tailFile 函数
package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
)

func TestTailFile_NotExist(t *testing.T) {
	lines, truncated, err := tailFile("/tmp/nonexistent_log_file_test.log", 200, 4*1024*1024)
	if err != nil {
		t.Fatalf("文件不存在不应报错: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("文件不存在应返回空数组，实际 %d 行", len(lines))
	}
	if truncated {
		t.Error("文件不存在时 truncated 应为 false")
	}
}

func TestTailFile_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	_ = os.WriteFile(path, []byte(""), 0644)

	lines, truncated, err := tailFile(path, 200, 4*1024*1024)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("空文件应返回空数组，实际 %d 行", len(lines))
	}
	if truncated {
		t.Error("空文件时 truncated 应为 false")
	}
}

func TestTailFile_SmallFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "small.log")
	content := "line1\nline2\nline3\n"
	_ = os.WriteFile(path, []byte(content), 0644)

	lines, truncated, err := tailFile(path, 200, 4*1024*1024)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("期望 3 行，实际 %d 行", len(lines))
	}
	if lines[0] != "line1" {
		t.Errorf("第一行期望 line1，实际 %s", lines[0])
	}
	if lines[2] != "line3" {
		t.Errorf("第三行期望 line3，实际 %s", lines[2])
	}
	if truncated {
		t.Error("小文件不应 truncated")
	}
}

func TestTailFile_LimitLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limit.log")
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString("line content here\n")
	}
	_ = os.WriteFile(path, []byte(sb.String()), 0644)

	result, _, err := tailFile(path, 10, 4*1024*1024)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(result) != 10 {
		t.Errorf("限制 10 行，实际 %d 行", len(result))
	}
}

func TestTailFile_LargeFile_Truncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.log")

	// 创建一个超过 maxBytes 的文件
	var sb strings.Builder
	for i := 0; i < 10000; i++ {
		sb.WriteString("this is a log line with some content to make it larger\n")
	}
	_ = os.WriteFile(path, []byte(sb.String()), 0644)

	maxBytes := int64(1024) // 限制 1KB 来触发截断
	result, truncated, err := tailFile(path, 100, maxBytes)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if !truncated {
		t.Error("大文件应 truncated=true")
	}
	if len(result) == 0 {
		t.Error("应返回至少 1 行")
	}
}

func TestServerClampReadBytesUsesConfig(t *testing.T) {
	cfg := app.DefaultConfig()
	cfg.Agent.MaxReadSize = "2M"
	server := &Server{cfg: cfg}

	got := server.clampReadBytes(8*1024*1024, 4*1024*1024)
	if got != 2*1024*1024 {
		t.Fatalf("期望按配置钳制到 2M，实际 %d", got)
	}
}

func TestTailFile_ExactMaxLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exact.log")
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("line content here\n")
	}
	_ = os.WriteFile(path, []byte(sb.String()), 0644)

	result, _, err := tailFile(path, 50, 4*1024*1024)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(result) != 50 {
		t.Errorf("期望 50 行，实际 %d 行", len(result))
	}
}

func TestTailFile_OverMaxLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "over.log")
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("line content here\n")
	}
	_ = os.WriteFile(path, []byte(sb.String()), 0644)

	result, _, err := tailFile(path, 50, 4*1024*1024)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(result) != 50 {
		t.Errorf("限制 50 行，实际 %d 行", len(result))
	}
}
