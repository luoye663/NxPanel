package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

type failingLogReopener struct{}

func (failingLogReopener) Reopen(ctx context.Context) (CmdResult, error) {
	return CmdResult{}, errors.New("reopen failed")
}

func createLogFile(t *testing.T, dir, name string, size int, modTime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	data := make([]byte, size)
	for i := range data {
		data[i] = 'x'
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("设置时间失败: %v", err)
	}
}

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var count int
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}

func fileExists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// ========== rotateLogs 测试 ==========

func TestRotateLogs_SkipsEmptyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewNginxExecutorWithDefaults("", "")

	createLogFile(t, tmpDir, "site.access.log", 0, time.Now())
	createLogFile(t, tmpDir, "site.error.log", 0, time.Now())

	rotateLogs(tmpDir, executor, 0, t.TempDir())

	if !fileExists(t, tmpDir, "site.access.log") {
		t.Error("空文件不应被切割")
	}
	if !fileExists(t, tmpDir, "site.error.log") {
		t.Error("空文件不应被切割")
	}
	if got := countFiles(t, tmpDir); got != 2 {
		t.Errorf("期望 2 个文件（未被切割），实际 %d 个", got)
	}
}

func TestRotateLogs_RenamesNonEmptyFiles(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewNginxExecutorWithDefaults("", "")

	now := time.Now()
	createLogFile(t, tmpDir, "site.access.log", 100, now)
	createLogFile(t, tmpDir, "site.error.log", 50, now)
	createLogFile(t, tmpDir, "other.conf", 200, now)

	rotateLogs(tmpDir, executor, 0, t.TempDir())

	entries, _ := os.ReadDir(tmpDir)
	var rotatedFiles []string
	for _, e := range entries {
		if !e.IsDir() {
			rotatedFiles = append(rotatedFiles, e.Name())
		}
	}

	foundAccess := false
	foundError := false
	for _, name := range rotatedFiles {
		if len(name) > len("site.access.log") && name[:len("site.access.log")] == "site.access.log" && name != "site.access.log" {
			foundAccess = true
		}
		if len(name) > len("site.error.log") && name[:len("site.error.log")] == "site.error.log" && name != "site.error.log" {
			foundError = true
		}
	}

	if !foundAccess {
		t.Error("非空 access.log 应被重命名")
	}
	if !foundError {
		t.Error("非空 error.log 应被重命名")
	}
	if !fileExists(t, tmpDir, "other.conf") {
		t.Error("非日志文件不应被移动")
	}
	if got := countFiles(t, tmpDir); got != 3 {
		t.Errorf("期望 3 个文件（2 个被重命名 + 1 个非日志文件），实际 %d 个", got)
	}
}

func TestRotateLogs_ReopenFailureReturned(t *testing.T) {
	tmpDir := t.TempDir()
	createLogFile(t, tmpDir, "site.access.log", 100, time.Now())

	result := rotateLogs(tmpDir, failingLogReopener{}, 0, t.TempDir())

	if result.RotatedCount != 1 {
		t.Fatalf("期望切割 1 个文件，实际 %d", result.RotatedCount)
	}
	if result.ReopenOK {
		t.Fatal("reopen 失败时 reopen_ok 应为 false")
	}
	if result.Message == "" {
		t.Fatal("reopen 失败时应返回风险提示 message")
	}
}

func TestRotateLogs_SkipsNonLogFiles(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewNginxExecutorWithDefaults("", "")

	createLogFile(t, tmpDir, "config.txt", 100, time.Now())
	createLogFile(t, tmpDir, "site.access.log.20260101_000000", 100, time.Now())

	rotateLogs(tmpDir, executor, 0, t.TempDir())

	if !fileExists(t, tmpDir, "config.txt") {
		t.Error("非日志文件不应被移动")
	}
	if !fileExists(t, tmpDir, "site.access.log.20260101_000000") {
		t.Error("已切割文件不应被再次切割")
	}
}

// ========== rotatedBaseName 测试 ==========

func TestRotatedBaseName(t *testing.T) {
	tests := []struct {
		input string
		base  string
		isRot bool
	}{
		{"site.access.log.20260101_120000", "site.access.log", true},
		{"site.error.log.20260101_120000", "site.error.log", true},
		{"site.access.log", "", false},
		{"site.error.log", "", false},
		{"other.conf", "", false},
		{"site.access.log.20260101_120000.gz", "site.access.log", true},
	}

	for _, tt := range tests {
		base, isRot := rotatedBaseName(tt.input)
		if base != tt.base || isRot != tt.isRot {
			t.Errorf("rotatedBaseName(%q) = (%q, %v), want (%q, %v)", tt.input, base, isRot, tt.base, tt.isRot)
		}
	}
}

// ========== cleanupRotatedLogs 测试 ==========

func TestCleanupRotatedLogs_CountNotExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 5; i++ {
		createLogFile(t, tmpDir, "site.access.log."+now.Add(-time.Duration(i)*time.Hour).Format("20060102_150405"), 10, now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupRotatedLogs(tmpDir, 10, 1*time.Hour)

	if got := countFiles(t, tmpDir); got != 5 {
		t.Errorf("数量未超 max_count，不应删除，期望 5 个文件，实际 %d 个", got)
	}
}

func TestCleanupRotatedLogs_CountExceededButAgeNotExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(i) * time.Minute).Format("20060102_150405")
		createLogFile(t, tmpDir, "site.access.log."+ts, 10, now.Add(-time.Duration(i)*time.Minute))
	}

	cleanupRotatedLogs(tmpDir, 5, 24*time.Hour)

	if got := countFiles(t, tmpDir); got != 10 {
		t.Errorf("数量超了但时间未超，不应删除，期望 10 个文件，实际 %d 个", got)
	}
}

func TestCleanupRotatedLogs_BothConditionsMet(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(i) * time.Hour).Format("20060102_150405")
		createLogFile(t, tmpDir, "site.access.log."+ts, 10, now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupRotatedLogs(tmpDir, 5, 3*time.Hour)

	remaining := countFiles(t, tmpDir)
	if remaining != 5 {
		t.Errorf("期望 5 个文件（最新 5 个保留，其余都超龄被删），实际 %d 个", remaining)
	}
}

func TestCleanupRotatedLogs_MixedAgeInOverflow(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 5; i++ {
		ts := now.Add(-time.Duration(i) * time.Minute).Format("20060102_150405")
		createLogFile(t, tmpDir, "site.access.log."+ts, 10, now.Add(-time.Duration(i)*time.Minute))
	}

	createLogFile(t, tmpDir, "site.access.log."+now.Add(-3*time.Hour).Format("20060102_150405"), 10, now.Add(-3*time.Hour))
	createLogFile(t, tmpDir, "site.access.log."+now.Add(-50*time.Hour).Format("20060102_150405"), 10, now.Add(-50*time.Hour))
	createLogFile(t, tmpDir, "site.access.log."+now.Add(-100*time.Hour).Format("20060102_150405"), 10, now.Add(-100*time.Hour))

	cleanupRotatedLogs(tmpDir, 5, 4*time.Hour)

	remaining := countFiles(t, tmpDir)
	if remaining != 6 {
		t.Errorf("期望 6 个文件（5 个最新 + 1 个溢出但时间未超），实际 %d 个", remaining)
	}
}

func TestCleanupRotatedLogs_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	cleanupRotatedLogs(tmpDir, 5, 1*time.Hour)
	if got := countFiles(t, tmpDir); got != 0 {
		t.Errorf("空目录应保持为空，实际 %d 个", got)
	}
}

func TestCleanupRotatedLogs_NonExistentDir(t *testing.T) {
	cleanupRotatedLogs("/tmp/nonexistent_log_dir_test_12345", 5, 1*time.Hour)
}

func TestCleanupRotatedLogs_MaxCountZero(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(i) * time.Hour).Format("20060102_150405")
		createLogFile(t, tmpDir, "site.access.log."+ts, 10, now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupRotatedLogs(tmpDir, 0, 1*time.Hour)

	if got := countFiles(t, tmpDir); got != 10 {
		t.Errorf("maxCount=0 应禁用清理，期望 10 个文件，实际 %d 个", got)
	}
}

func TestCleanupRotatedLogs_GroupByBaseName(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 8; i++ {
		ts := now.Add(-time.Duration(i) * time.Hour).Format("20060102_150405")
		createLogFile(t, tmpDir, "a.access.log."+ts, 10, now.Add(-time.Duration(i)*time.Hour))
		createLogFile(t, tmpDir, "b.error.log."+ts, 10, now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupRotatedLogs(tmpDir, 5, 3*time.Hour)

	remaining := countFiles(t, tmpDir)
	if remaining != 10 {
		t.Errorf("期望 10 个文件（每组保留 5 个），实际 %d 个", remaining)
	}
}

func TestCleanupRotatedLogs_IgnoresActiveLogs(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	createLogFile(t, tmpDir, "site.access.log", 100, now)
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(i) * time.Hour).Format("20060102_150405")
		createLogFile(t, tmpDir, "site.access.log."+ts, 10, now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupRotatedLogs(tmpDir, 5, 3*time.Hour)

	if !fileExists(t, tmpDir, "site.access.log") {
		t.Error("活跃日志文件不应被清理")
	}
	remaining := countFiles(t, tmpDir)
	if remaining != 6 {
		t.Errorf("期望 6 个文件（1 活跃 + 5 切割保留），实际 %d 个", remaining)
	}
}

// ========== isActiveLogFile 测试 ==========

func TestIsActiveLogFile(t *testing.T) {
	tests := []struct {
		name   string
		active bool
	}{
		{"site.access.log", true},
		{"site.error.log", true},
		{"site.access.log.20260101_120000", false},
		{"site.error.log.20260101_120000", false},
		{"other.conf", false},
		{"access.log", false},
	}

	for _, tt := range tests {
		if got := isActiveLogFile(tt.name); got != tt.active {
			t.Errorf("isActiveLogFile(%q) = %v, want %v", tt.name, got, tt.active)
		}
	}
}

func TestRotateLogs_MinSize(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewNginxExecutorWithDefaults("", "")
	now := time.Now()

	createLogFile(t, tmpDir, "small.access.log", 50, now)
	createLogFile(t, tmpDir, "big.access.log", 200, now)

	rotateLogs(tmpDir, executor, 100, t.TempDir())

	if !fileExists(t, tmpDir, "small.access.log") {
		t.Error("小于 minSize 的文件不应被切割")
	}

	smallCount := 0
	bigRotated := false
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		if e.Name() == "small.access.log" {
			smallCount++
		}
		if len(e.Name()) > len("big.access.log") && e.Name()[:len("big.access.log")] == "big.access.log" && e.Name() != "big.access.log" {
			bigRotated = true
		}
	}

	if smallCount != 1 {
		t.Error("小文件应保持不变")
	}
	if !bigRotated {
		t.Error("大于 minSize 的文件应被切割")
	}
}

func TestRotateLogs_MinSizeZero(t *testing.T) {
	tmpDir := t.TempDir()
	executor := NewNginxExecutorWithDefaults("", "")
	now := time.Now()

	createLogFile(t, tmpDir, "site.access.log", 10, now)

	rotateLogs(tmpDir, executor, 0, t.TempDir())

	if fileExists(t, tmpDir, "site.access.log") {
		t.Error("minSize=0 时，非空文件应被切割")
	}
}

func TestRunNginxLogRotation_EmptyLogDir(t *testing.T) {
	logDir := t.TempDir()
	dataDir := t.TempDir()
	s := &Server{
		cfg: &app.Config{DataDir: dataDir, Nginx: app.NginxConfig{
			LogDir: logDir,
		}},
		policy:   NewPathPolicy([]string{logDir, dataDir}),
		executor: NewNginxExecutorWithDefaults("", ""),
	}

	result, err := s.runNginxLogRotation(NginxLogRotateRunRequest{MinSize: "0", MaxCount: 10, MaxAge: "24h"})
	if err != nil {
		t.Fatalf("空日志目录不应失败: %v", err)
	}
	if result.RotatedCount != 0 || result.RemovedCount != 0 || !result.ReopenOK {
		t.Fatalf("空日志目录结果不符合预期: %+v", result)
	}
	if !fileExists(t, filepath.Join(dataDir, "logs", "tasks"), "log_rotation.log") {
		t.Fatal("即使没有可切割文件，也应写入 Nginx 日志切割任务日志")
	}
}

func TestRunNginxLogRotation_InvalidParams(t *testing.T) {
	s := &Server{cfg: &app.Config{Nginx: app.NginxConfig{LogDir: t.TempDir()}}, policy: NewPathPolicy([]string{t.TempDir()}), executor: NewNginxExecutorWithDefaults("", "")}

	if _, err := s.runNginxLogRotation(NginxLogRotateRunRequest{MinSize: "11G", MaxCount: 10, MaxAge: "24h"}); err == nil {
		t.Fatal("超过 10G 的 min_size 应被拒绝")
	}
	if _, err := s.runNginxLogRotation(NginxLogRotateRunRequest{MinSize: "0", MaxCount: 0, MaxAge: "24h"}); err == nil {
		t.Fatal("max_count=0 应被拒绝")
	}
	if _, err := s.runNginxLogRotation(NginxLogRotateRunRequest{MinSize: "0", MaxCount: 10, MaxAge: "30m"}); err == nil {
		t.Fatal("小于 1h 的 max_age 应被拒绝")
	}
}
