package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
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
	var req struct{}
	if !DecodeJSONOptional(w, r, &req) {
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
	if err := s.agentClient.ControlNginx(r.Context(), action); err != nil {
		_ = s.opRepo.UpdateError(opID, "failed", app.ErrInternalError, err.Error(), "")
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, "Nginx "+action+" 失败", map[string]any{"stderr": err.Error(), "operation_id": opID})
		return
	}
	_ = s.opRepo.UpdateStatus(opID, "success")
	WriteOK(w, r, map[string]any{"ok": true, "operation_id": opID})
}
