package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type rotatedFileInfo struct {
	name    string
	modTime time.Time
}

type LogRotationResult struct {
	RotatedCount int    `json:"rotated_count"`
	RemovedCount int    `json:"removed_count"`
	ReopenOK     bool   `json:"reopen_ok"`
	Message      string `json:"message"`
}

type nginxLogReopener interface {
	Reopen(ctx context.Context) (CmdResult, error)
}

func rotateLogs(logDir string, executor nginxLogReopener, minSize int64, taskLogDir string) LogRotationResult {
	result := LogRotationResult{ReopenOK: true}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("读取日志目录失败", "path", logDir, "error", err)
			result.Message = "读取日志目录失败: " + err.Error()
		}
		return result
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !isActiveLogFile(name) {
			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		if info.Size() == 0 {
			continue
		}

		if minSize > 0 && info.Size() < minSize {
			continue
		}

		src := filepath.Join(logDir, name)
		ts := info.ModTime().Format("20060102_150405")
		dst := src + "." + ts

		if _, err := os.Stat(dst); err == nil {
			continue
		}

		if err := os.Rename(src, dst); err != nil {
			slog.Error("切割日志文件失败", "src", src, "dst", dst, "error", err)
			writeTaskLog(taskLogConfig{logDir: taskLogDir, fileName: "log_rotation_" + baseLogName(name) + ".log"}, "error", fmt.Sprintf("切割失败: %s → %s: %v", src, dst, err))
			continue
		}
		result.RotatedCount++
		slog.Info("日志文件已切割", "src", src, "dst", dst)
		writeTaskLog(taskLogConfig{logDir: taskLogDir, fileName: "log_rotation_" + baseLogName(name) + ".log"}, "info", fmt.Sprintf("已切割: %s → %s", src, dst))
	}

	if result.RotatedCount > 0 {
		if _, err := executor.Reopen(context.Background()); err != nil {
			slog.Error("nginx -s reopen 失败", "error", err)
			writeTaskLog(taskLogConfig{logDir: taskLogDir, fileName: "log_rotation.log"}, "error", fmt.Sprintf("nginx -s reopen 失败: %v", err))
			result.ReopenOK = false
			result.Message = fmt.Sprintf("nginx -s reopen 失败，Nginx 可能继续写入旧日志文件句柄: %v", err)
		} else {
			slog.Info("nginx -s reopen 成功", "rotated", result.RotatedCount)
			writeTaskLog(taskLogConfig{logDir: taskLogDir, fileName: "log_rotation.log"}, "info", fmt.Sprintf("nginx -s reopen 成功, 切割 %d 个文件", result.RotatedCount))
		}
	}
	if result.Message == "" {
		result.Message = fmt.Sprintf("日志切割完成，切割 %d 个文件", result.RotatedCount)
	}
	return result
}

func isActiveLogFile(name string) bool {
	return strings.HasSuffix(name, ".access.log") || strings.HasSuffix(name, ".error.log")
}

func baseLogName(name string) string {
	s := name
	for _, suffix := range []string{".access.log", ".error.log"} {
		if strings.HasSuffix(s, suffix) {
			return strings.TrimSuffix(s, suffix)
		}
	}
	return strings.TrimSuffix(s, filepath.Ext(s))
}

func cleanupRotatedLogs(logDir string, maxCount int, maxAge time.Duration) int {
	if maxCount <= 0 || maxAge <= 0 {
		return 0
	}

	entries, err := os.ReadDir(logDir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("读取日志目录失败", "path", logDir, "error", err)
		}
		return 0
	}

	groups := make(map[string][]rotatedFileInfo)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		baseName, ok := rotatedBaseName(name)
		if !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		groups[baseName] = append(groups[baseName], rotatedFileInfo{
			name:    name,
			modTime: info.ModTime(),
		})
	}

	cutoff := time.Now().Add(-maxAge)
	var totalRemoved int

	for baseName, files := range groups {
		if len(files) <= maxCount {
			continue
		}

		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime.After(files[j].modTime)
		})

		for i := maxCount; i < len(files); i++ {
			if files[i].modTime.Before(cutoff) {
				fullPath := filepath.Join(logDir, files[i].name)
				if err := os.Remove(fullPath); err != nil {
					slog.Error("清理切割日志失败", "path", fullPath, "error", err)
					continue
				}
				slog.Info("已清理旧切割日志", "path", fullPath, "base", baseName, "mod_time", files[i].modTime.Format(time.RFC3339))
				totalRemoved++
			}
		}
	}

	if totalRemoved > 0 {
		slog.Info("切割日志清理完成", "removed", totalRemoved)
	}
	return totalRemoved
}

func rotatedBaseName(name string) (string, bool) {
	if strings.HasSuffix(name, ".access.log") || strings.HasSuffix(name, ".error.log") {
		return "", false
	}
	for _, suffix := range []string{".access.log", ".error.log"} {
		idx := strings.Index(name, suffix+".")
		if idx >= 0 {
			return name[:idx+len(suffix)], true
		}
	}
	return "", false
}

type NginxLogRotateRunRequest struct {
	MinSize  string `json:"min_size"`
	MaxCount int    `json:"max_count"`
	MaxAge   string `json:"max_age"`
}

func (s *Server) handleNginxLogRotateRun(w http.ResponseWriter, r *http.Request) {
	var req NginxLogRotateRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	result, err := s.runNginxLogRotation(req)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAgentOK(w, result)
}

func (s *Server) runNginxLogRotation(req NginxLogRotateRunRequest) (LogRotationResult, error) {
	// RPC 不接受 log_dir，只使用 Agent 本地配置，避免被滥用为任意目录重命名工具。
	if err := validateNginxLogRotateRunRequest(req); err != nil {
		return LogRotationResult{}, err
	}
	logDir := strings.TrimSpace(s.cfg.Nginx.LogDir)
	if logDir == "" {
		return LogRotationResult{}, fmt.Errorf("nginx.log_dir 为空，无法执行日志切割")
	}
	if _, err := s.policy.Validate(logDir); err != nil {
		return LogRotationResult{}, fmt.Errorf("日志目录不在 Agent 白名单内: %w", err)
	}
	maxAge, _ := time.ParseDuration(strings.TrimSpace(req.MaxAge))
	maxCount := req.MaxCount
	minSize, _ := parseLogRotateSize(req.MinSize)
	taskLogDir := s.cfg.TaskLogDir()
	ensureTaskLogDir(taskLogDir)
	result := rotateLogs(logDir, s.executor, minSize, taskLogDir)
	result.RemovedCount = cleanupRotatedLogs(logDir, maxCount, maxAge)
	if result.Message == "" {
		result.Message = fmt.Sprintf("日志切割完成，切割 %d 个文件，清理 %d 个文件", result.RotatedCount, result.RemovedCount)
	}
	level := "info"
	if !result.ReopenOK {
		level = "error"
	}
	writeTaskLog(taskLogConfig{logDir: taskLogDir, fileName: "log_rotation.log"}, level, fmt.Sprintf("Nginx 网站日志切割执行完成: %s，清理 %d 个文件", result.Message, result.RemovedCount))
	return result, nil
}

func validateNginxLogRotateRunRequest(req NginxLogRotateRunRequest) error {
	if req.MaxCount < 1 || req.MaxCount > 1000 {
		return fmt.Errorf("max_count 必须在 1-1000 之间")
	}
	maxAge, err := time.ParseDuration(strings.TrimSpace(req.MaxAge))
	if err != nil {
		return fmt.Errorf("max_age 格式无效，例如: 720h")
	}
	if maxAge < time.Hour || maxAge > 8760*time.Hour {
		return fmt.Errorf("max_age 必须在 1h-8760h 之间")
	}
	if _, err := parseLogRotateSize(req.MinSize); err != nil {
		return err
	}
	return nil
}

func parseLogRotateSize(value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("min_size 不能为空")
	}
	s := strings.ToUpper(strings.TrimSpace(value))
	if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(s, "G"):
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "M"):
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		multiplier = 1024
		s = strings.TrimSuffix(s, "K")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("min_size 格式无效，例如: 0, 100M, 1G")
	}
	size := n * multiplier
	if size > 10*1024*1024*1024 {
		return 0, fmt.Errorf("min_size 不能超过 10G")
	}
	return size, nil
}
