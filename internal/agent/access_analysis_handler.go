package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/luoye663/nxpanel/internal/accessanalysis"
)

func (s *Server) handleAccessAnalysisScan(w http.ResponseWriter, r *http.Request) {
	var req accessanalysis.AgentScanRequest
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
	from, err := time.Parse(time.RFC3339, req.FromTime)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "from_time 格式错误")
		return
	}
	to, err := time.Parse(time.RFC3339, req.ToTime)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, "to_time 格式错误")
		return
	}
	parser, err := accessanalysis.NewParser(req.Format, req.CustomPattern, req.NormalizeQuery)
	if err != nil {
		writeAgentError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.MaxBytes <= 0 {
		req.MaxBytes = 64 * 1024 * 1024
	}
	if req.MaxLines <= 0 {
		req.MaxLines = 500000
	}

	agg := accessanalysis.NewAggregator(from, to)
	result := accessanalysis.AgentScanResponse{}
	paths := []string{req.Path}
	if req.IncludeRotated {
		paths = append(paths, rotatedLogCandidates(req.Path)...)
	}
	for _, path := range paths {
		if _, err := s.policy.Validate(path); err != nil {
			continue
		}
		cursor := accessanalysis.Cursor{}
		if path == req.Path {
			cursor = req.Cursor
		}
		fileCursor, scanned, skipped, truncated, errors := scanAccessLogFile(r.Context(), path, parser, agg, cursor, req.MaxBytes, req.MaxLines)
		result.ScannedLines += scanned
		result.SkippedLines += skipped
		result.Truncated = result.Truncated || truncated
		if len(result.ParseErrors) < 20 {
			result.ParseErrors = append(result.ParseErrors, errors...)
			if len(result.ParseErrors) > 20 {
				result.ParseErrors = result.ParseErrors[:20]
			}
		}
		if path == req.Path {
			result.Cursor = fileCursor
		}
		if result.Truncated {
			break
		}
	}
	aggResult := agg.Result()
	result.Hourly = aggResult.Hourly
	result.Paths = aggResult.Paths
	result.IPs = aggResult.IPs
	result.EntriesSample = aggResult.EntriesSample
	result.Anomalies = aggResult.Anomalies
	writeAgentOK(w, result)
}

func (s *Server) handleAccessAnalysisFormatDetect(w http.ResponseWriter, r *http.Request) {
	var req accessanalysis.AgentFormatDetectRequest
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
	if req.MaxLines <= 0 || req.MaxLines > 50 {
		req.MaxLines = 20
	}
	sample, err := readHeadLines(req.Path, req.MaxLines)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取日志样本失败: "+err.Error())
		return
	}
	writeAgentOK(w, accessanalysis.DetectFormatFromSample(sample))
}

func scanAccessLogFile(ctxDone interface{ Done() <-chan struct{} }, path string, parser *accessanalysis.Parser, agg *accessanalysis.Aggregator, cursor accessanalysis.Cursor, maxBytes, maxLines int64) (accessanalysis.Cursor, int64, int64, bool, []string) {
	file, err := os.Open(path)
	if err != nil {
		return cursor, 0, 0, false, []string{err.Error()}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return cursor, 0, 0, false, []string{err.Error()}
	}
	inode := inodeOf(info)
	startOffset := cursor.Offset
	if cursor.Inode != inode || cursor.FileSize > info.Size() || startOffset < 0 || startOffset > info.Size() {
		startOffset = 0
	}
	if startOffset > 0 {
		_, _ = file.Seek(startOffset, 0)
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	var scanned, skipped, readBytes int64
	parseErrors := []string{}
	truncated := false
	for scanned < maxLines && readBytes < maxBytes {
		select {
		case <-ctxDone.Done():
			truncated = true
			return accessanalysis.Cursor{Inode: inode, Offset: startOffset + readBytes, FileSize: info.Size()}, scanned, skipped, truncated, parseErrors
		default:
		}
		line, err := reader.ReadString('\n')
		if line == "" && err != nil {
			break
		}
		readBytes += int64(len(line))
		if len(line) > 32*1024 {
			skipped++
			continue
		}
		entry, parseErr := parser.ParseLine(line)
		if parseErr != nil {
			skipped++
			if len(parseErrors) < 20 {
				parseErrors = append(parseErrors, parseErr.Error())
			}
			continue
		}
		if agg.Add(entry) {
			scanned++
		}
		if err != nil {
			break
		}
	}
	if scanned >= maxLines || readBytes >= maxBytes {
		truncated = true
	}
	return accessanalysis.Cursor{Inode: inode, Offset: startOffset + readBytes, FileSize: info.Size()}, scanned, skipped, truncated, parseErrors
}

func inodeOf(info os.FileInfo) uint64 {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino
	}
	return 0
}

func rotatedLogCandidates(path string) []string {
	matches, _ := filepath.Glob(path + "*")
	items := []string{}
	for _, item := range matches {
		if item != path && !strings.HasSuffix(item, ".tmp") {
			items = append(items, item)
		}
	}
	sort.Strings(items)
	return items
}

func readHeadLines(path string, maxLines int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer file.Close()
	reader := bufio.NewScanner(file)
	reader.Buffer(make([]byte, 4096), 64*1024)
	lines := []string{}
	for reader.Scan() && len(lines) < maxLines {
		lines = append(lines, reader.Text())
	}
	if err := reader.Err(); err != nil {
		return "", fmt.Errorf("读取样本失败: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}
