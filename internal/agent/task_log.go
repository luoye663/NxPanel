package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

func writeTaskLog(cfg taskLogConfig, level, message string) {
	taskDir := cfg.logDir
	if taskDir == "" {
		return
	}

	ts := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s [%s] %s\n", ts, level, message)

	logPath := filepath.Join(taskDir, cfg.fileName)

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		slog.Error("写入任务日志失败", "path", logPath, "error", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(line); err != nil {
		slog.Error("写入任务日志失败", "path", logPath, "error", err)
	}
}

type taskLogConfig struct {
	logDir   string
	fileName string
}

func ensureTaskLogDir(dir string) {
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		slog.Error("创建任务日志目录失败", "path", dir, "error", err)
	}
}
