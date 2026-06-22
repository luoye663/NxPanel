// api 包 — log handler
//
// 处理日志接口：
//   - GET  /api/v1/sites/{site_id}/logs            查看日志尾部
//   - POST /api/v1/sites/{site_id}/logs/truncate    清空日志
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/logs"
)

// handleLogGet 查看日志尾部
func (s *Server) handleLogGet(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	logType := r.URL.Query().Get("type")
	if logType == "" {
		logType = "access"
	}

	linesStr := r.URL.Query().Get("lines")
	lines := 200
	if linesStr != "" {
		if n, err := strconv.Atoi(linesStr); err == nil {
			lines = n
		}
	}

	result, err := s.logsSvc.Get(r.Context(), siteID, logType, lines)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

// handleLogTruncate 清空日志
func (s *Server) handleLogTruncate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req logs.TruncateRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.logsSvc.Truncate(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleLogDownload(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	logType := queryLogType(r)
	rotated := r.URL.Query().Get("rotated")
	resp, filename, err := s.logsSvc.Download(r.Context(), siteID, logType, rotated)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleLogSearch(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	lines := parseLines(r, 200)
	result, err := s.logsSvc.Search(r.Context(), siteID, queryLogType(r), r.URL.Query().Get("q"), r.URL.Query().Get("rotated"), lines)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleRotatedLogList(w http.ResponseWriter, r *http.Request) {
	result, err := s.logsSvc.RotatedList(r.Context(), chi.URLParam(r, "site_id"), queryLogType(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleRotatedLogTail(w http.ResponseWriter, r *http.Request) {
	result, err := s.logsSvc.RotatedTail(r.Context(), chi.URLParam(r, "site_id"), queryLogType(r), r.URL.Query().Get("name"), parseLines(r, 200))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleRotatedLogDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.logsSvc.DeleteRotated(r.Context(), chi.URLParam(r, "site_id"), queryLogType(r), r.URL.Query().Get("name"), middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"ok": true})
}

func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Time{})
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, r, http.StatusInternalServerError, "STREAM_UNSUPPORTED", "当前连接不支持实时日志", nil)
		return
	}
	logType := queryLogType(r)
	from := r.URL.Query().Get("from")
	last := ""
	if from != "end" {
		if result, err := s.logsSvc.Get(r.Context(), chi.URLParam(r, "site_id"), logType, 50); err == nil {
			for _, line := range result.Lines {
				writeSSELine(w, "line", line)
				last = line
			}
			flusher.Flush()
		}
	}
	ticker := time.NewTicker(2 * time.Second)
	heartbeat := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			writeSSELine(w, "heartbeat", "{}")
			flusher.Flush()
		case <-ticker.C:
			result, err := s.logsSvc.Get(r.Context(), chi.URLParam(r, "site_id"), logType, 50)
			if err != nil {
				writeSSELine(w, "error", err.Error())
				flusher.Flush()
				continue
			}
			for _, line := range newLinesAfter(result.Lines, last) {
				writeSSELine(w, "line", line)
				last = line
			}
			flusher.Flush()
		}
	}
}

func queryLogType(r *http.Request) string {
	if value := r.URL.Query().Get("type"); value != "" {
		return value
	}
	return "access"
}

func parseLines(r *http.Request, fallback int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get("lines")); err == nil && n > 0 {
		return n
	}
	return fallback
}

func writeSSELine(w io.Writer, event, data string) {
	_, _ = fmt.Fprintf(w, "event: %s\n", event)
	encoded, _ := json.Marshal(map[string]string{"line": data})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func newLinesAfter(lines []string, last string) []string {
	if last == "" {
		return lines
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == last {
			return lines[i+1:]
		}
	}
	return lines
}
