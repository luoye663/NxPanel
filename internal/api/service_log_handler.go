package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
)

func (s *Server) handleServiceLogTail(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Agent 不可用", nil)
		return
	}

	service := r.URL.Query().Get("service")
	if service == "" {
		service = "api"
	}
	if service != "api" && service != "agent" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "service 必须是 api 或 agent", nil)
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 {
		lines = 1000
	}
	if lines > 2000 {
		// API 侧先限制最大行数，避免异常请求继续放大到 Agent 和响应体。
		lines = 2000
	}

	resp, err := s.agentClient.ServiceLogTail(r.Context(), &agentclient.ServiceLogRequest{
		Service:  service,
		MaxLines: lines,
	})
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	WriteOK(w, r, resp)
}

func (s *Server) handleServiceLogClear(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Agent 不可用", nil)
		return
	}

	service := r.URL.Query().Get("service")
	if service == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "service 参数不能为空", nil)
		return
	}
	if service != "api" && service != "agent" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "service 必须是 api 或 agent", nil)
		return
	}

	if err := s.agentClient.ServiceLogTruncate(r.Context(), service); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	WriteOK(w, r, map[string]any{"ok": true})
}

func (s *Server) handleServiceLogStream(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Agent 不可用", nil)
		return
	}
	service := r.URL.Query().Get("service")
	if service == "" {
		service = "api"
	}
	if service != "api" && service != "agent" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "service 必须是 api 或 agent", nil)
		return
	}
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

	last := ""
	if r.URL.Query().Get("from") != "end" {
		if resp, err := s.agentClient.ServiceLogTail(r.Context(), &agentclient.ServiceLogRequest{Service: service, MaxLines: 50}); err == nil {
			for _, line := range resp.Lines {
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
			resp, err := s.agentClient.ServiceLogTail(r.Context(), &agentclient.ServiceLogRequest{Service: service, MaxLines: 50})
			if err != nil {
				writeSSELine(w, "error", err.Error())
				flusher.Flush()
				continue
			}
			// 与站点日志追踪一致：根据最后一行去重，只把新增内容推给前端。
			for _, line := range newLinesAfter(resp.Lines, last) {
				writeSSELine(w, "line", line)
				last = line
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleTaskLogTypes(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Agent 不可用", nil)
		return
	}

	resp, err := s.agentClient.TaskLogList(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	WriteOK(w, r, resp)
}

func (s *Server) handleTaskLogTail(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Agent 不可用", nil)
		return
	}

	name := r.URL.Query().Get("task")
	if name == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "task 参数不能为空", nil)
		return
	}
	lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if lines <= 0 {
		lines = 500
	}

	resp, err := s.agentClient.TaskLogTail(r.Context(), &agentclient.TaskLogRequest{
		Name:     name,
		MaxLines: lines,
	})
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	WriteOK(w, r, resp)
}

func (s *Server) handleTaskLogClear(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Agent 不可用", nil)
		return
	}

	name := r.URL.Query().Get("task")
	if name == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "task 参数不能为空", nil)
		return
	}

	if err := s.agentClient.TaskLogTruncate(r.Context(), name); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	WriteOK(w, r, map[string]any{"ok": true})
}
