package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	limits := s.accessScanLimits()
	req.MaxBytes = clampRequested(req.MaxBytes, limits.maxBytes)
	req.MaxLines = clampRequested(req.MaxLines, limits.maxLines)
	aggregationLimits := requestedAggregationLimits(&req, limits.aggregation)
	ctx, cancel := context.WithTimeout(r.Context(), limits.timeout)
	defer cancel()

	agg := accessanalysis.NewAggregatorWithLimits(from, to, aggregationLimits)
	result := accessanalysis.AgentScanResponse{}
	paths := []string{req.Path}
	if req.IncludeRotated {
		paths = append(paths, rotatedLogCandidates(req.Path, limits.rotatedFiles)...)
	}
	budget := &accessScanBudget{bytesRemaining: req.MaxBytes, linesRemaining: req.MaxLines}
	for _, path := range paths {
		if _, err := s.policy.Validate(path); err != nil {
			continue
		}
		cursor := accessanalysis.Cursor{}
		if path == req.Path {
			cursor = req.Cursor
		}
		fileCursor, scanned, skipped, truncated, errors := scanAccessLogFile(ctx, path, parser, agg, cursor, budget, limits.maxLineBytes)
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
	result.Truncation = aggResult.Truncation
	result.Truncated = result.Truncated || aggResult.Truncation.Truncated
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
	sample, err := readHeadLines(req.Path, req.MaxLines, s.accessScanLimits().maxLineBytes)
	if err != nil {
		writeAgentError(w, http.StatusInternalServerError, "读取日志样本失败: "+err.Error())
		return
	}
	writeAgentOK(w, accessanalysis.DetectFormatFromSample(sample))
}

type accessScanBudget struct {
	bytesRemaining int64
	linesRemaining int64
}

func clampRequested(value, maximum int64) int64 {
	if value <= 0 || value > maximum {
		return maximum
	}
	return value
}

func clampRequestedInt(value, maximum int) int {
	if value <= 0 || value > maximum {
		return maximum
	}
	return value
}

func requestedAggregationLimits(req *accessanalysis.AgentScanRequest, configured accessanalysis.AggregationLimits) accessanalysis.AggregationLimits {
	if !req.CollectEntries {
		req.MaxEntries = 0
		configured.Entries = 0
		return configured
	}
	req.MaxEntries = clampRequestedInt(req.MaxEntries, configured.Entries)
	configured.Entries = req.MaxEntries
	return configured
}

func scanAccessLogFile(ctx context.Context, path string, parser *accessanalysis.Parser, agg *accessanalysis.Aggregator, cursor accessanalysis.Cursor, budget *accessScanBudget, maxLineBytes int) (accessanalysis.Cursor, int64, int64, bool, []string) {
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

	limited := &io.LimitedReader{R: file, N: budget.bytesRemaining}
	reader := bufio.NewReaderSize(limited, maxLineBytes+1)
	var scanned, skipped, readBytes int64
	parseErrors := []string{}
	truncated := false
	for budget.linesRemaining > 0 && budget.bytesRemaining > 0 {
		select {
		case <-ctx.Done():
			truncated = true
			return accessanalysis.Cursor{Inode: inode, Offset: startOffset + readBytes, FileSize: info.Size()}, scanned, skipped, truncated, parseErrors
		default:
		}
		lineStart := readBytes
		line, consumed, oversized, err := readBoundedLine(reader, maxLineBytes)
		if consumed == 0 && err != nil {
			break
		}
		readBytes += consumed
		budget.bytesRemaining -= consumed
		if err == io.EOF && limited.N == 0 && startOffset+readBytes < info.Size() && (len(line) == 0 || line[len(line)-1] != '\n') {
			// The request byte budget ended inside this physical line. Rewind the
			// persisted cursor so the next scan reparses the complete line.
			return accessanalysis.Cursor{Inode: inode, Offset: startOffset + lineStart, FileSize: info.Size()}, scanned, skipped, true, parseErrors
		}
		budget.linesRemaining--
		if oversized {
			skipped++
			if err != nil && err != io.EOF {
				parseErrors = appendParseError(parseErrors, err)
			}
			continue
		}
		entry, parseErr := parser.ParseLine(string(line))
		if parseErr != nil {
			skipped++
			if len(parseErrors) < 20 {
				parseErrors = append(parseErrors, accessanalysis.TruncateUTF8(parseErr.Error(), accessanalysis.MaxAnomalyReasonBytes))
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
	if budget.linesRemaining <= 0 || budget.bytesRemaining <= 0 || ctx.Err() != nil {
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

func rotatedLogCandidates(path string, maxFiles int) []string {
	matches, _ := filepath.Glob(path + "*")
	items := []string{}
	for _, item := range matches {
		if item != path && !strings.HasSuffix(item, ".tmp") {
			items = append(items, item)
		}
	}
	sort.Strings(items)
	if maxFiles >= 0 && len(items) > maxFiles {
		items = items[len(items)-maxFiles:]
	}
	return items
}

func readHeadLines(path string, maxLines, maxLineBytes int) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	defer file.Close()
	reader := bufio.NewScanner(file)
	reader.Buffer(make([]byte, 4096), maxLineBytes)
	lines := []string{}
	for reader.Scan() && len(lines) < maxLines {
		lines = append(lines, reader.Text())
	}
	if err := reader.Err(); err != nil {
		return "", fmt.Errorf("读取样本失败: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}

func readBoundedLine(reader *bufio.Reader, maxLineBytes int) ([]byte, int64, bool, error) {
	line := make([]byte, 0, min(maxLineBytes, 4096))
	var consumed int64
	oversized := false
	for {
		fragment, err := reader.ReadSlice('\n')
		consumed += int64(len(fragment))
		if !oversized {
			remaining := maxLineBytes - len(line)
			if len(fragment) > remaining {
				if remaining > 0 {
					line = append(line, fragment[:remaining]...)
				}
				oversized = true
			} else {
				line = append(line, fragment...)
			}
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			return line, consumed, oversized, err
		}
	}
}

func appendParseError(current []string, err error) []string {
	if err != nil && len(current) < 20 {
		return append(current, accessanalysis.TruncateUTF8(err.Error(), accessanalysis.MaxAnomalyReasonBytes))
	}
	return current
}
