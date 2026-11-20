package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

func (s *Server) handleNginxStatus(w http.ResponseWriter, r *http.Request) {
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "Agent 不可用", nil)
		return
	}
	result, err := s.agentClient.NginxStatus(r.Context())
	if err != nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, err.Error(), nil)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleNginxControl(w http.ResponseWriter, r *http.Request) {
	action := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if action != "start" && action != "stop" && action != "restart" {
		WriteError(w, r, http.StatusBadRequest, app.ErrValidationFailed, "无效操作", nil)
		return
	}
	var req nginx.RuntimeControlRequest
	if !DecodeJSONOptional(w, r, &req) {
		return
	}
	if !req.ValidFor(action) {
		WriteError(w, r, http.StatusBadRequest, app.ErrValidationFailed, "无效的 Nginx 控制请求", nil)
		return
	}
	if s.agentClient == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "Agent 不可用", nil)
		return
	}
	opID := app.NewOperationID()
	op := &repo.Operation{ID: opID, Action: "nginx." + action, TargetType: "system", TargetID: "nginx", Status: "pending", RequestID: middleware.GetRequestID(r.Context()), Actor: "admin", Message: "Nginx " + action, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	if err := s.opRepo.Create(op); err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, "无法创建操作审计记录，未执行操作", nil)
		return
	}
	if err := s.agentClient.ControlNginx(r.Context(), action, req); err != nil {
		status, code, message := http.StatusInternalServerError, app.ErrInternalError, "Nginx "+action+" 失败"
		var agentErr *agentclient.HTTPError
		details := map[string]any{"operation_id": opID}
		stderr := err.Error()
		if errors.As(err, &agentErr) {
			stderr = agentErr.Message
			if agentErr.StatusCode == http.StatusConflict {
				status, code, message = http.StatusConflict, app.ErrConflict, agentErr.Message
				stderr = ""
			}
		}
		if stderr != "" {
			details["stderr"] = stderr
		}
		_ = s.opRepo.UpdateError(opID, "failed", code, message, stderr)
		WriteError(w, r, status, code, message, details)
		return
	}
	_ = s.opRepo.UpdateStatus(opID, "success")
	WriteOK(w, r, map[string]any{"ok": true, "operation_id": opID})
}
