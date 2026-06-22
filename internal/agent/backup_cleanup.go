package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type backupDirInfo struct {
	name    string
	modTime time.Time
}

func cleanupOldBackups(backupBase string, maxCount int, maxAge time.Duration) {
	if maxCount <= 0 || maxAge <= 0 {
		return
	}

	entries, err := os.ReadDir(backupBase)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("读取备份目录失败", "path", backupBase, "error", err)
		}
		return
	}

	var dirs []backupDirInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, backupDirInfo{
			name:    e.Name(),
			modTime: info.ModTime(),
		})
	}

	if len(dirs) <= maxCount {
		return
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.After(dirs[j].modTime)
	})

	cutoff := time.Now().Add(-maxAge)
	var removed int
	for i := maxCount; i < len(dirs); i++ {
		if dirs[i].modTime.Before(cutoff) {
			fullPath := filepath.Join(backupBase, dirs[i].name)
			if err := os.RemoveAll(fullPath); err != nil {
				slog.Error("清理备份目录失败", "path", fullPath, "error", err)
				continue
			}
			slog.Info("已清理旧备份目录", "path", fullPath, "mod_time", dirs[i].modTime.Format(time.RFC3339))
			removed++
		}
	}

	if removed > 0 {
		slog.Info("备份清理完成", "total", len(dirs), "removed", removed, "remaining", len(dirs)-removed)
	}
}

func cleanupOldBackupsWithLog(backupBase string, maxCount int, maxAge time.Duration, taskLogDir string) {
	if maxCount <= 0 || maxAge <= 0 {
		return
	}

	ensureTaskLogDir(taskLogDir)
	tlc := taskLogConfig{logDir: taskLogDir, fileName: "backup_cleanup.log"}

	entries, err := os.ReadDir(backupBase)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("读取备份目录失败", "path", backupBase, "error", err)
			writeTaskLog(tlc, "error", fmt.Sprintf("读取备份目录失败: %v", err))
		}
		return
	}

	var dirs []backupDirInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		dirs = append(dirs, backupDirInfo{
			name:    e.Name(),
			modTime: info.ModTime(),
		})
	}

	if len(dirs) <= maxCount {
		return
	}

	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].modTime.After(dirs[j].modTime)
	})

	cutoff := time.Now().Add(-maxAge)
	var removed int
	for i := maxCount; i < len(dirs); i++ {
		if dirs[i].modTime.Before(cutoff) {
			fullPath := filepath.Join(backupBase, dirs[i].name)
			if err := os.RemoveAll(fullPath); err != nil {
				slog.Error("清理备份目录失败", "path", fullPath, "error", err)
				writeTaskLog(tlc, "error", fmt.Sprintf("清理备份失败 %s: %v", fullPath, err))
				continue
			}
			slog.Info("已清理旧备份目录", "path", fullPath, "mod_time", dirs[i].modTime.Format(time.RFC3339))
			removed++
		}
	}

	if removed > 0 {
		msg := fmt.Sprintf("备份清理完成: total=%d, removed=%d, remaining=%d", len(dirs), removed, len(dirs)-removed)
		slog.Info(msg)
		writeTaskLog(tlc, "info", msg)
	}
}
