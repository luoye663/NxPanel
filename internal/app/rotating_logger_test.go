package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRotatingFileWriter_RotatesBySize(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "api.log")
	w, err := NewRotatingFileWriter(logPath, ServiceLogRotateConfig{Enabled: true, MaxSize: "10", MaxCount: 30, MaxAge: "720h", Interval: "1m"})
	if err != nil {
		t.Fatalf("创建轮转 writer 失败: %v", err)
	}
	defer w.Close()

	if _, err := w.Write([]byte("1234567890")); err != nil {
		t.Fatalf("写入日志失败: %v", err)
	}
	if _, err := w.Write([]byte("abc")); err != nil {
		t.Fatalf("触发切割写入失败: %v", err)
	}

	active, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取活跃日志失败: %v", err)
	}
	if string(active) != "abc" {
		t.Fatalf("活跃日志内容应为新写入内容，实际 %q", string(active))
	}
	if countRotatedFiles(t, dir, "api.log.") != 1 {
		t.Fatalf("应产生 1 个切割文件")
	}
}

func TestRotatingFileWriter_CleanupRequiresCountAndAge(t *testing.T) {
	oldTime := time.Now().Add(-48 * time.Hour)
	recentTime := time.Now()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "api.log")
	createRotatedFile(t, dir, "api.log.20260608_202000", oldTime)
	createRotatedFile(t, dir, "api.log.20260608_202001", oldTime)
	if err := cleanupRotatedServiceLogs(logPath, 3, time.Hour); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if countRotatedFiles(t, dir, "api.log.") != 2 {
		t.Fatalf("数量未超过 max_count 时不应删除")
	}

	dir = t.TempDir()
	logPath = filepath.Join(dir, "api.log")
	createRotatedFile(t, dir, "api.log.20260608_202000", recentTime)
	createRotatedFile(t, dir, "api.log.20260608_202001", recentTime.Add(time.Second))
	createRotatedFile(t, dir, "api.log.20260608_202002", recentTime.Add(2*time.Second))
	if err := cleanupRotatedServiceLogs(logPath, 2, 24*time.Hour); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if countRotatedFiles(t, dir, "api.log.") != 3 {
		t.Fatalf("超过数量但未超过 max_age 的文件不应删除")
	}

	dir = t.TempDir()
	logPath = filepath.Join(dir, "api.log")
	createRotatedFile(t, dir, "api.log.20260608_202000", oldTime)
	createRotatedFile(t, dir, "api.log.20260608_202001", oldTime.Add(time.Second))
	createRotatedFile(t, dir, "api.log.20260608_202002", recentTime)
	createRotatedFile(t, dir, "api.log.20260608_202003", recentTime.Add(time.Second))
	if err := cleanupRotatedServiceLogs(logPath, 2, time.Hour); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if countRotatedFiles(t, dir, "api.log.") != 2 {
		t.Fatalf("数量和时间双条件满足后应删除旧文件")
	}
}

func TestRotatingFileWriter_CloseIdempotent(t *testing.T) {
	w, err := NewRotatingFileWriter(filepath.Join(t.TempDir(), "api.log"), ServiceLogRotateConfig{Enabled: true, MaxSize: "1M", Interval: "1m"})
	if err != nil {
		t.Fatalf("创建轮转 writer 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("首次 Close 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("重复 Close 应保持幂等，实际: %v", err)
	}
}

func TestRotatingFileWriter_DisabledFallback(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "api.log")
	logger, closer := InitLoggerToFileWithRotation("info", "text", logPath, ServiceLogRotateConfig{Enabled: false})
	defer closer.Close()
	logger.Info("fallback works")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	if !strings.Contains(string(data), "fallback works") {
		t.Fatalf("禁用轮转时仍应写入普通日志文件，实际: %s", string(data))
	}
}

func TestServiceLogRotatedBaseFilter(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "api.log")
	oldTime := time.Now().Add(-48 * time.Hour)
	createRotatedFile(t, dir, "api.log.20260608_202000", oldTime)
	createRotatedFile(t, dir, "api.log.20260608_202001", oldTime)
	createRotatedFile(t, dir, "agent.log.20260608_202000", oldTime)
	createRotatedFile(t, dir, "api.log.custom", oldTime)

	if err := cleanupRotatedServiceLogs(logPath, 1, time.Hour); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.log.20260608_202000")); err != nil {
		t.Fatalf("API 清理不应影响 Agent 切割文件: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "api.log.custom")); err != nil {
		t.Fatalf("时间戳无法解析的文件不应删除: %v", err)
	}
}

func TestInitLoggerToFileWithRotationWritesJSON(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "agent.log")
	logger, closer := InitLoggerToFileWithRotation("info", "json", logPath, ServiceLogRotateConfig{Enabled: true, MaxSize: "1M", Interval: "1m"})
	defer closer.Close()
	logger.LogAttrs(context.Background(), slog.LevelInfo, "json works")

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("读取日志失败: %v", err)
	}
	if !strings.Contains(string(data), "json works") {
		t.Fatalf("轮转 writer 应写入 JSON 日志，实际: %s", string(data))
	}
}

func createRotatedFile(t *testing.T, dir, name string, modTime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("x"), 0644); err != nil {
		t.Fatalf("创建切割文件失败: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("设置切割文件时间失败: %v", err)
	}
}

func countRotatedFiles(t *testing.T, dir, prefix string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) {
			count++
		}
	}
	return count
}
