package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func createBackupDir(t *testing.T, base, name, content string, modTime time.Time) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, "test.conf"), []byte(content), 0644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
	}
	if err := os.Chtimes(dir, modTime, modTime); err != nil {
		t.Fatalf("设置时间失败: %v", err)
	}
}

func countDirs(t *testing.T, base string) int {
	t.Helper()
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0
	}
	var count int
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	return count
}

func TestCleanupOldBackups_CountNotExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 5; i++ {
		createBackupDir(t, tmpDir, fmt.Sprintf("op_%03d", i), "content", now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupOldBackups(tmpDir, 10, 1*time.Hour)

	if got := countDirs(t, tmpDir); got != 5 {
		t.Errorf("数量未超 max_count，不应删除，期望 5 个目录，实际 %d 个", got)
	}
}

func TestCleanupOldBackups_CountExceededButAgeNotExceeded(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 10; i++ {
		createBackupDir(t, tmpDir, fmt.Sprintf("op_%03d", i), "content", now.Add(-time.Duration(i)*time.Minute))
	}

	cleanupOldBackups(tmpDir, 5, 24*time.Hour)

	if got := countDirs(t, tmpDir); got != 10 {
		t.Errorf("数量超了但时间未超，不应删除，期望 10 个目录，实际 %d 个", got)
	}
}

func TestCleanupOldBackups_BothConditionsMet(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	for i := 0; i < 10; i++ {
		createBackupDir(t, tmpDir, fmt.Sprintf("op_%03d", i), "content", now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupOldBackups(tmpDir, 5, 3*time.Hour)

	remaining := countDirs(t, tmpDir)
	if remaining != 5 {
		t.Errorf("期望 5 个目录（最新 5 个保留，其余都超龄被删），实际 %d 个", remaining)
	}
}

func TestCleanupOldBackups_MixedAgeInOverflow(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	createBackupDir(t, tmpDir, "op_000", "content", now.Add(-1*time.Minute))
	createBackupDir(t, tmpDir, "op_001", "content", now.Add(-2*time.Minute))
	createBackupDir(t, tmpDir, "op_002", "content", now.Add(-3*time.Minute))
	createBackupDir(t, tmpDir, "op_003", "content", now.Add(-4*time.Minute))
	createBackupDir(t, tmpDir, "op_004", "content", now.Add(-5*time.Minute))
	createBackupDir(t, tmpDir, "op_old_1", "content", now.Add(-50*time.Hour))
	createBackupDir(t, tmpDir, "op_old_2", "content", now.Add(-100*time.Hour))
	createBackupDir(t, tmpDir, "op_recent_overflow", "content", now.Add(-3*time.Hour))

	cleanupOldBackups(tmpDir, 5, 4*time.Hour)

	remaining := countDirs(t, tmpDir)
	if remaining != 6 {
		entries, _ := os.ReadDir(tmpDir)
		var names []string
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		t.Errorf("期望 6 个目录（5个最新 + op_recent_overflow 时间未超），实际 %d 个: %v", remaining, names)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "op_recent_overflow")); os.IsNotExist(err) {
		t.Error("op_recent_overflow 时间未超龄，不应被删除")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "op_old_1")); !os.IsNotExist(err) {
		t.Error("op_old_1 数量超且时间超龄，应被删除")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "op_old_2")); !os.IsNotExist(err) {
		t.Error("op_old_2 数量超且时间超龄，应被删除")
	}
}

func TestCleanupOldBackups_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	cleanupOldBackups(tmpDir, 5, 1*time.Hour)
	if got := countDirs(t, tmpDir); got != 0 {
		t.Errorf("空目录应保持为空，实际 %d 个", got)
	}
}

func TestCleanupOldBackups_NonExistentDir(t *testing.T) {
	cleanupOldBackups("/tmp/nonexistent_backup_dir_test_12345", 5, 1*time.Hour)
}

func TestCleanupOldBackups_MaxCountZero(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()
	for i := 0; i < 10; i++ {
		createBackupDir(t, tmpDir, fmt.Sprintf("op_%03d", i), "content", now.Add(-time.Duration(i)*time.Hour))
	}

	cleanupOldBackups(tmpDir, 0, 1*time.Hour)

	if got := countDirs(t, tmpDir); got != 10 {
		t.Errorf("maxCount=0 应禁用清理，期望 10 个目录，实际 %d 个", got)
	}
}

func TestCleanupOldBackups_FilesIgnored(t *testing.T) {
	tmpDir := t.TempDir()
	now := time.Now()

	if err := os.WriteFile(filepath.Join(tmpDir, "readme.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	for i := 0; i < 8; i++ {
		createBackupDir(t, tmpDir, fmt.Sprintf("op_%03d", i), "content", now.Add(-time.Duration(i+5)*time.Hour))
	}

	cleanupOldBackups(tmpDir, 3, 6*time.Hour)

	if _, err := os.Stat(filepath.Join(tmpDir, "readme.txt")); os.IsNotExist(err) {
		t.Error("普通文件不应被删除")
	}
}
