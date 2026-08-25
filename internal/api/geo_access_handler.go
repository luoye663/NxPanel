package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/geoaccess"
)

func (s *Server) requireGeoAccess(w http.ResponseWriter, r *http.Request) bool {
	if s.geoAccessSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "地域访问服务不可用", nil)
		return false
	}
	return true
}

func (s *Server) handleGeoIPSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	result, err := s.geoAccessSvc.GetSettings()
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleGeoIPSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	var req geoaccess.UpdateGeoIPSettingsRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.geoAccessSvc.UpdateSettings(r.Context(), req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleGeoIPStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	result, err := s.geoAccessSvc.GetStatus()
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleGeoIPUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	result, err := s.geoAccessSvc.UpdateDatabase(r.Context(), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleGeoIPDatabaseUpload(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65*1024*1024)
	if err := r.ParseMultipartForm(65 * 1024 * 1024); err != nil {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "上传内容无效", nil)
		return
	}
	file, _, err := r.FormFile("database")
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "请选择 MMDB 文件", nil)
		return
	}
	defer file.Close()
	result, err := s.geoAccessSvc.InstallDatabase(r.Context(), io.LimitReader(file, 65*1024*1024), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func geoSiteID(r *http.Request) string { return strings.TrimSpace(chi.URLParam(r, "site_id")) }
func (s *Server) handleSiteGeoAccessGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	result, err := s.geoAccessSvc.GetSiteAccess(geoSiteID(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleSiteGeoAccessUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	var req geoaccess.UpdateSiteAccessRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.geoAccessSvc.UpdateSiteAccess(r.Context(), geoSiteID(r), req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleSiteGeoAccessEnable(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	result, err := s.geoAccessSvc.EnableSite(r.Context(), geoSiteID(r), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleSiteGeoAccessDisable(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	result, err := s.geoAccessSvc.DisableSite(r.Context(), geoSiteID(r), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleSiteGeoRuleList(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	result, err := s.geoAccessSvc.ListRules(geoSiteID(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleSiteGeoRuleCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	var req geoaccess.CreateRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.geoAccessSvc.CreateRule(r.Context(), geoSiteID(r), req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteCreated(w, r, result)
}
func (s *Server) handleSiteGeoRuleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	var req geoaccess.UpdateRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.geoAccessSvc.UpdateRule(r.Context(), geoSiteID(r), chi.URLParam(r, "rule_id"), req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleSiteGeoRuleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	err := s.geoAccessSvc.DeleteRule(r.Context(), geoSiteID(r), chi.URLParam(r, "rule_id"), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]bool{"deleted": true})
}
func (s *Server) handleSiteGeoRuleReorder(w http.ResponseWriter, r *http.Request) {
	if !s.requireGeoAccess(w, r) {
		return
	}
	var req geoaccess.ReorderRulesRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.geoAccessSvc.ReorderRules(r.Context(), geoSiteID(r), req.RuleIDs, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
