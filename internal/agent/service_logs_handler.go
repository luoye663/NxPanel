package agent

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type serviceLogRequest struct {
	Service  string `json:"service"`
	MaxLines int    `json:"max_lines"`
}

type taskLogListResponse struct {
	Tasks []taskLogEntry `json:"tasks"`
}

type taskLogEntry struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Label   string `json:"label"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

type taskLogRequest struct {
	Name     string `json:"name"`
	MaxLines int    `json:"max_lines"`
}

func (s *Server) handleServiceLogTail(w http.ResponseWriter, r *http.Request) {
	var req serviceLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	logPath, ok := s.serviceLogPath(req.Service)
	if !ok {
		writeAgentError(w, http.StatusBadRequest, "service 必须是 api 或 agent")
		return
	}

	if req.MaxLines <= 0 {
		req.MaxLines = 1000
	}
	if req.MaxLines > 2000 {
		// Agent 是最终防线，即使 API 被绕过也不能读取过多运行日志。
		req.MaxLines = 2000
	}

	lines, truncated, err := tailFile(logPath, req.MaxLines, s.clampReadBytes(8*1024*1024, 8*1024*1024))
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取日志失败: "+err.Error())
		return
	}

	writeAgentOK(w, map[string]any{
		"lines":     lines,
		"truncated": truncated,
		"path":      logPath,
	})
}

func (s *Server) handleServiceLogTruncate(w http.ResponseWriter, r *http.Request) {
	var req serviceLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	logPath, ok := s.serviceLogPath(req.Service)
	if !ok {
		writeAgentError(w, http.StatusBadRequest, "service 必须是 api 或 agent")
		return
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		writeAgentOK(w, map[string]any{"ok": true})
		return
	}

	if err := os.Truncate(logPath, 0); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "清空日志失败: "+err.Error())
		return
	}

	writeAgentOK(w, map[string]any{"ok": true})
}

func (s *Server) serviceLogPath(service string) (string, bool) {
	// 这里复用配置层路径解析，确保运行日志读取/清空和进程实际写入路径保持一致。
	switch strings.ToLower(service) {
	case "api":
		return s.cfg.APILogPath(), true
	case "agent":
		return s.cfg.AgentLogPath(), true
	default:
		return "", false
	}
}

func (s *Server) handleTaskLogList(w http.ResponseWriter, r *http.Request) {
	taskDir := s.cfg.TaskLogDir()

	entries, err := os.ReadDir(taskDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeAgentOK(w, taskLogListResponse{Tasks: []taskLogEntry{}})
			return
		}
		writeAgentError(w, http.StatusInternalServerError, "读取任务日志目录失败: "+err.Error())
		return
	}

	var tasks []taskLogEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}

		name := e.Name()
		taskType, label := parseTaskLogName(name)

		tasks = append(tasks, taskLogEntry{
			Name:    name,
			Type:    taskType,
			Label:   label,
			Size:    info.Size(),
			ModTime: info.ModTime().UTC().Format(time.RFC3339),
		})
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].ModTime > tasks[j].ModTime
	})

	if tasks == nil {
		tasks = []taskLogEntry{}
	}

	writeAgentOK(w, taskLogListResponse{Tasks: tasks})
}

func (s *Server) handleTaskLogTail(w http.ResponseWriter, r *http.Request) {
	var req taskLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if req.Name == "" {
		writeAgentError(w, http.StatusBadRequest, "name 不能为空")
		return
	}

	if strings.Contains(req.Name, "/") || strings.Contains(req.Name, "\\") || strings.Contains(req.Name, "..") {
		writeAgentError(w, http.StatusBadRequest, "name 包含非法字符")
		return
	}

	if !strings.HasSuffix(req.Name, ".log") {
		writeAgentError(w, http.StatusBadRequest, "name 必须以 .log 结尾")
		return
	}

	logPath := filepath.Join(s.cfg.TaskLogDir(), req.Name)

	if _, err := s.policy.Validate(logPath); err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不允许: "+err.Error())
		return
	}

	if req.MaxLines <= 0 {
		req.MaxLines = 500
	}
	if req.MaxLines > 2000 {
		req.MaxLines = 2000
	}

	lines, truncated, err := tailFile(logPath, req.MaxLines, s.clampReadBytes(4*1024*1024, 4*1024*1024))
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取任务日志失败: "+err.Error())
		return
	}

	writeAgentOK(w, map[string]any{
		"lines":     lines,
		"truncated": truncated,
	})
}

func (s *Server) handleTaskLogTruncate(w http.ResponseWriter, r *http.Request) {
	var req taskLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if req.Name == "" {
		writeAgentError(w, http.StatusBadRequest, "name 不能为空")
		return
	}

	if strings.Contains(req.Name, "/") || strings.Contains(req.Name, "\\") || strings.Contains(req.Name, "..") {
		writeAgentError(w, http.StatusBadRequest, "name 包含非法字符")
		return
	}

	if !strings.HasSuffix(req.Name, ".log") {
		writeAgentError(w, http.StatusBadRequest, "name 必须以 .log 结尾")
		return
	}

	logPath := filepath.Join(s.cfg.TaskLogDir(), req.Name)

	if _, err := s.policy.Validate(logPath); err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不允许: "+err.Error())
		return
	}

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		writeAgentOK(w, map[string]any{"ok": true})
		return
	}

	if err := os.Truncate(logPath, 0); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "清空任务日志失败: "+err.Error())
		return
	}

	writeAgentOK(w, map[string]any{"ok": true})
}

func parseTaskLogName(filename string) (taskType, label string) {
	base := strings.TrimSuffix(filename, ".log")

	if strings.HasPrefix(base, "log_rotation_") {
		domain := strings.TrimPrefix(base, "log_rotation_")
		return "log_rotation", "切割日志 [" + domain + "]"
	}
	if base == "log_rotation" {
		return "nginx_log_rotation", "Nginx 网站日志切割"
	}
	if strings.HasPrefix(base, "ssl_renewal_") {
		domain := strings.TrimPrefix(base, "ssl_renewal_")
		return "ssl_renewal", "SSL续签 [" + domain + "]"
	}
	if base == "acme_renewal" {
		return "acme_renewal", "SSL 自动续签检查"
	}
	if base == "access_analysis_scan" {
		return "access_analysis_scan", "访问分析扫描"
	}
	if base == "site_backup" {
		return "site_backup", "站点备份"
	}
	if base == "backup_cleanup" {
		return "backup_cleanup", "备份清理"
	}
	return base, base
}
