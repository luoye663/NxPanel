// agent 包 — 日志 RPC handler
//
// 处理 agent 端的日志操作：
//   - POST /internal/v1/logs/tail      读取日志尾部
//   - POST /internal/v1/logs/truncate  清空日志文件
//
// 日志文件路径由 API 层从 DB 中获取，agent 不接受用户自定义路径。
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// logTailRequest 日志 tail 请求
type logTailRequest struct {
	Path     string `json:"path"`
	MaxLines int    `json:"max_lines"`
	MaxBytes int64  `json:"max_bytes"`
}

// logTailResponse 日志 tail 响应
type logTailResponse struct {
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated"`
}

// logTruncateRequest 日志 truncate 请求
type logTruncateRequest struct {
	Path string `json:"path"`
}

type logSearchRequest struct {
	Path          string `json:"path"`
	Keyword       string `json:"keyword"`
	MaxBytes      int64  `json:"max_bytes"`
	MaxLines      int    `json:"max_lines"`
	CaseSensitive bool   `json:"case_sensitive"`
}

type logSearchResponse struct {
	Lines     []string `json:"lines"`
	Matched   int      `json:"matched"`
	Truncated bool     `json:"truncated"`
	MaxBytes  int64    `json:"max_bytes"`
}

type rotatedLogListRequest struct {
	BasePath string `json:"base_path"`
}

type rotatedLogTailRequest struct {
	BasePath string `json:"base_path"`
	Name     string `json:"name"`
	MaxLines int    `json:"max_lines"`
	MaxBytes int64  `json:"max_bytes"`
}

type rotatedLogRemoveRequest struct {
	BasePath string `json:"base_path"`
	Name     string `json:"name"`
}

type rotatedLogItem struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	ModTime    string `json:"mod_time"`
	Compressed bool   `json:"compressed"`
}

// handleLogTail 读取日志文件尾部
//
// 从文件末尾按块倒读，最大读取 maxBytes 字节、maxLines 行。
// 文件不存在时返回空数组。
func (s *Server) handleLogTail(w http.ResponseWriter, r *http.Request) {
	var req logTailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if req.Path == "" {
		writeAgentError(w, http.StatusBadRequest, "path 不能为空")
		return
	}

	// 默认值
	if req.MaxLines <= 0 {
		req.MaxLines = 200
	}
	if req.MaxLines > 1000 {
		req.MaxLines = 1000
	}
	// max_bytes 来自 API/前端请求，Agent 再做一次配置化硬钳制，避免被人为调大后分配超大内存。
	req.MaxBytes = s.clampReadBytes(req.MaxBytes, 4*1024*1024)

	// 安全校验：路径必须在允许范围内
	path, err := s.policy.Validate(req.Path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不允许: "+err.Error())
		return
	}

	lines, truncated, err := tailFile(path, req.MaxLines, req.MaxBytes)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取日志失败: "+err.Error())
		return
	}

	writeAgentOK(w, logTailResponse{
		Lines:     lines,
		Truncated: truncated,
	})
}

// handleLogTruncate 清空日志文件
//
// 使用 os.Truncate(path, 0) 清空文件内容，不删除文件。
func (s *Server) handleLogTruncate(w http.ResponseWriter, r *http.Request) {
	var req logTruncateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	if req.Path == "" {
		writeAgentError(w, http.StatusBadRequest, "path 不能为空")
		return
	}

	// 安全校验
	if _, err := s.policy.Validate(req.Path); err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不允许: "+err.Error())
		return
	}

	// 检查文件是否存在
	info, err := os.Stat(req.Path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在视为成功
			writeAgentOK(w, map[string]any{"ok": true})
			return
		}
		writeAgentError(w, http.StatusInternalServerError, "检查文件失败: "+err.Error())
		return
	}

	// 不清空过大文件（超过 100MB 拒绝，防止异常）
	if info.Size() > 100*1024*1024 {
		writeAgentError(w, http.StatusBadRequest, "文件过大，拒绝清空")
		return
	}

	if err := os.Truncate(req.Path, 0); err != nil {
		writeAgentError(w, http.StatusInternalServerError, "清空文件失败: "+err.Error())
		return
	}

	slog.Info("日志文件已清空", "path", req.Path)
	writeAgentOK(w, map[string]any{"ok": true})
}

// handleLogDownload 流式下载日志文件，避免 API 或 agent 把大日志一次性读入内存。
func (s *Server) handleLogDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeAgentError(w, http.StatusBadRequest, "path 不能为空")
		return
	}
	path, err := s.policy.Validate(path)
	if err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不允许: "+err.Error())
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeAgentError(w, http.StatusNotFound, "日志文件不存在")
			return
		}
		writeAgentError(w, http.StatusInternalServerError, "检查日志失败: "+err.Error())
		return
	}
	if info.IsDir() {
		writeAgentError(w, http.StatusBadRequest, "不能下载目录")
		return
	}
	maxDownloadBytes := s.maxDownloadBytes()
	if info.Size() > maxDownloadBytes {
		writeAgentError(w, http.StatusBadRequest, fmt.Sprintf("日志文件过大，最多允许下载 %d MB", maxDownloadBytes/(1024*1024)))
		return
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeAgentError(w, http.StatusNotFound, "日志文件不存在")
			return
		}
		writeAgentError(w, http.StatusInternalServerError, "打开日志失败: "+err.Error())
		return
	}
	defer f.Close()

	filename := filepath.Base(path)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	ctx, cancel := context.WithTimeout(r.Context(), s.downloadTimeout())
	defer cancel()
	if _, err := copyWithContext(ctx, w, io.LimitReader(f, info.Size())); err != nil {
		slog.Warn("日志下载中断", "path", path, "error", err)
	}
}

func (s *Server) handleLogSearch(w http.ResponseWriter, r *http.Request) {
	var req logSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	if req.Path == "" {
		writeAgentError(w, http.StatusBadRequest, "path 不能为空")
		return
	}
	if _, err := s.policy.Validate(req.Path); err != nil {
		writeAgentError(w, http.StatusForbidden, "路径不允许: "+err.Error())
		return
	}
	lines, matched, truncated, maxBytes, err := searchLogFile(req.Path, req.Keyword, req.MaxLines, req.MaxBytes, req.CaseSensitive)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "搜索日志失败: "+err.Error())
		return
	}
	writeAgentOK(w, logSearchResponse{Lines: lines, Matched: matched, Truncated: truncated, MaxBytes: maxBytes})
}

func (s *Server) handleRotatedLogList(w http.ResponseWriter, r *http.Request) {
	var req rotatedLogListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	items, err := listRotatedLogs(req.BasePath, s.policy)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"items": items})
}

func (s *Server) handleRotatedLogTail(w http.ResponseWriter, r *http.Request) {
	var req rotatedLogTailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	path, err := resolveRotatedLogPath(req.BasePath, req.Name, s.policy)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.MaxBytes = s.clampReadBytes(req.MaxBytes, 4*1024*1024)
	lines, truncated, err := tailFile(path, req.MaxLines, req.MaxBytes)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取历史日志失败: "+err.Error())
		return
	}
	writeAgentOK(w, logTailResponse{Lines: lines, Truncated: truncated})
}

func (s *Server) handleRotatedLogRemove(w http.ResponseWriter, r *http.Request) {
	var req rotatedLogRemoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}
	path, err := resolveRotatedLogPath(req.BasePath, req.Name, s.policy)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		writeAgentError(w, http.StatusInternalServerError, "删除历史日志失败: "+err.Error())
		return
	}
	writeAgentOK(w, map[string]any{"ok": true})
}

// tailFile 从文件末尾倒读指定行数
//
// 实现方式：
//  1. 获取文件大小
//  2. 读取尾部最多 maxBytes 字节
//  3. 按换行符分割
//  4. 取最后 maxLines 行
func tailFile(path string, maxLines int, maxBytes int64) ([]string, bool, error) {
	if maxLines <= 0 {
		maxLines = 200
	}
	if maxLines > 1000 {
		maxLines = 1000
	}
	if maxBytes <= 0 {
		maxBytes = 4 * 1024 * 1024
	}
	if maxBytes > maxAgentLimitBytes {
		// 尾部读取会一次性申请 readSize 大小的 buffer，底层保留绝对上限防止误用。
		maxBytes = maxAgentLimitBytes
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, false, nil
		}
		return nil, false, err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	size := st.Size()

	// 确定读取范围
	readSize := maxBytes
	truncated := false
	if size < readSize {
		readSize = size
	} else if size > 0 {
		truncated = true
	}

	if readSize == 0 {
		return []string{}, false, nil
	}

	// 从文件末尾往前读取
	buf := make([]byte, readSize)
	_, err = f.ReadAt(buf, size-readSize)
	if err != nil && err != io.EOF {
		return nil, false, fmt.Errorf("读取文件失败: %w", err)
	}

	// 按换行符分割
	rawLines := bytes.Split(buf, []byte("\n"))

	// 从后往前收集非空行，最多 maxLines 行
	lines := make([]string, 0, maxLines)
	for i := len(rawLines) - 1; i >= 0 && len(lines) < maxLines; i-- {
		if len(rawLines[i]) == 0 {
			continue
		}
		lines = append(lines, string(rawLines[i]))
	}

	// 反转为正序
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	return lines, truncated, nil
}

func searchLogFile(path, keyword string, maxLines int, maxBytes int64, caseSensitive bool) ([]string, int, bool, int64, error) {
	if maxLines <= 0 {
		maxLines = 200
	}
	if maxLines > 1000 {
		maxLines = 1000
	}
	if maxBytes <= 0 {
		maxBytes = 8 * 1024 * 1024
	}
	if maxBytes > 64*1024*1024 {
		maxBytes = 64 * 1024 * 1024
	}
	if keyword == "" {
		lines, truncated, err := tailFile(path, maxLines, maxBytes)
		return lines, len(lines), truncated, maxBytes, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, 0, false, maxBytes, nil
		}
		return nil, 0, false, maxBytes, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, 0, false, maxBytes, err
	}
	readSize := maxBytes
	truncated := false
	if st.Size() < readSize {
		readSize = st.Size()
	} else if st.Size() > 0 {
		truncated = true
	}
	if readSize == 0 {
		return []string{}, 0, false, maxBytes, nil
	}
	buf := make([]byte, readSize)
	if _, err := f.ReadAt(buf, st.Size()-readSize); err != nil && err != io.EOF {
		return nil, 0, false, maxBytes, err
	}
	needle := []byte(keyword)
	if !caseSensitive {
		needle = bytes.ToLower(needle)
	}
	result := make([]string, 0, maxLines)
	for _, raw := range bytes.Split(buf, []byte("\n")) {
		lineForMatch := raw
		if !caseSensitive {
			lineForMatch = bytes.ToLower(raw)
		}
		if !bytes.Contains(lineForMatch, needle) {
			continue
		}
		// 复制匹配行，避免返回字符串长期引用整块读取 buffer。
		if len(raw) > 4096 {
			raw = raw[:4096]
			truncated = true
		}
		result = append(result, string(append([]byte(nil), raw...)))
		if len(result) >= maxLines {
			truncated = true
			break
		}
	}
	return result, len(result), truncated, maxBytes, nil
}

type pathPolicyValidator interface {
	Validate(path string) (string, error)
}

func listRotatedLogs(basePath string, policy pathPolicyValidator) ([]rotatedLogItem, error) {
	if basePath == "" {
		return nil, fmt.Errorf("base_path 不能为空")
	}
	if _, err := policy.Validate(basePath); err != nil {
		return nil, fmt.Errorf("路径不允许: %w", err)
	}
	pattern := filepath.Base(basePath) + ".*"
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(basePath), pattern))
	if err != nil {
		return nil, err
	}
	items := make([]rotatedLogItem, 0, len(matches))
	for _, path := range matches {
		name := filepath.Base(path)
		if path == basePath || !isRotatedLogName(basePath, name) {
			continue
		}
		if _, err := policy.Validate(path); err != nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		items = append(items, rotatedLogItem{Name: name, Path: path, Size: info.Size(), ModTime: info.ModTime().UTC().Format(time.RFC3339), Compressed: strings.HasSuffix(name, ".gz")})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ModTime > items[j].ModTime })
	return items, nil
}

func resolveRotatedLogPath(basePath, name string, policy pathPolicyValidator) (string, error) {
	if basePath == "" || name == "" {
		return "", fmt.Errorf("base_path 和 name 不能为空")
	}
	if name != filepath.Base(name) || strings.Contains(name, "..") || strings.ContainsAny(name, "/\\") {
		return "", fmt.Errorf("历史日志名称非法")
	}
	if !isRotatedLogName(basePath, name) {
		return "", fmt.Errorf("历史日志名称不匹配当前日志")
	}
	path := filepath.Join(filepath.Dir(basePath), name)
	if !strings.HasPrefix(path, basePath+".") {
		return "", fmt.Errorf("历史日志路径越界")
	}
	if _, err := policy.Validate(path); err != nil {
		return "", fmt.Errorf("路径不允许: %w", err)
	}
	return path, nil
}

func isRotatedLogName(basePath, name string) bool {
	baseName := filepath.Base(basePath)
	if !strings.HasPrefix(name, baseName+".") {
		return false
	}
	suffix := strings.TrimPrefix(name, baseName+".")
	suffix = strings.TrimSuffix(suffix, ".gz")
	if len(suffix) != len("20060102_150405") {
		return false
	}
	_, err := time.Parse("20060102_150405", suffix)
	return err == nil
}
