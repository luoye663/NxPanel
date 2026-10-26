package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/maintenance"
)

func (s *Server) maintenanceAvailable(w http.ResponseWriter, r *http.Request) bool {
	if s.maintenanceSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "Agent 不可用", nil)
		return false
	}
	return true
}
func (s *Server) handleMaintenanceGet(w http.ResponseWriter, r *http.Request) {
	if !s.maintenanceAvailable(w, r) {
		return
	}
	result, err := s.maintenanceSvc.Status(r.Context(), chi.URLParam(r, "site_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleMaintenanceUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.maintenanceAvailable(w, r) {
		return
	}
	var req maintenance.UpdateRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	id, err := s.maintenanceSvc.Update(r.Context(), chi.URLParam(r, "site_id"), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]string{"operation_id": id})
}
func (s *Server) handleMaintenanceDefaultsGet(w http.ResponseWriter, r *http.Request) {
	if !s.maintenanceAvailable(w, r) {
		return
	}
	result, err := s.maintenanceSvc.Defaults(r.Context())
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleMaintenanceDefaultsUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.maintenanceAvailable(w, r) {
		return
	}
	var req maintenance.DefaultsRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	id, err := s.maintenanceSvc.SetDefaults(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]string{"operation_id": id})
}

// Serialize authenticated mutations against maintenance transitions. The Agent
// independently protects active blocks for background jobs and direct RPCs.
func (s *Server) guardMaintenanceWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		s.configurationWriteMu.Lock()
		defer s.configurationWriteMu.Unlock()
		if err := r.Context().Err(); err != nil {
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/"+chi.URLParam(r, "gateSecret"))
		parts := strings.Split(strings.Trim(path, "/"), "/")
		blockedID := ""
		check := false
		if len(parts) >= 2 && parts[0] == "sites" && parts[1] != "import" && parts[1] != "import-scan" {
			blockedID = parts[1]
			check = true
			if len(parts) >= 3 && (parts[2] == "maintenance" || parts[2] == "logs" || parts[2] == "certificates") {
				check = false
			}
		}
		if strings.HasPrefix(path, "/files") || path == "/nginx/conf" || path == "/nginx/parameters" || strings.HasPrefix(path, "/settings/default-") {
			check = true
			blockedID = ""
		}
		if check {
			blocked, err := repo.NewMaintenanceRepo(s.db).Blocked(r.Context(), blockedID)
			if err != nil {
				WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, "读取维护状态失败", nil)
				return
			}
			if blocked {
				WriteError(w, r, http.StatusConflict, app.ErrConflict, "维护中或存在待对账操作：请先恢复访问；仅允许维护配置和安全证书局部更新", nil)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
