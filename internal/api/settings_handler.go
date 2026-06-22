package api

import (
	"net/http"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/settings"
)

func (s *Server) handleSettingsDefaultPagesGet(w http.ResponseWriter, r *http.Request) {
	pages, err := s.settingsSvc.GetDefaultPages()
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	WriteOK(w, r, pages)
}

func (s *Server) handleSettingsDefaultPagesUpdate(w http.ResponseWriter, r *http.Request) {
	var req settings.UpdateDefaultPagesRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	requestID := middleware.GetRequestID(r.Context())
	if err := s.settingsSvc.UpdateDefaultPages(r.Context(), &req, requestID); err != nil {
		writeAppError(w, r, err)
		return
	}
	pages, _ := s.settingsSvc.GetDefaultPages()
	WriteOK(w, r, pages)
}

func (s *Server) handleSettingsDefaultSiteGet(w http.ResponseWriter, r *http.Request) {
	ds, err := s.settingsSvc.GetDefaultSite()
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	WriteOK(w, r, ds)
}

func (s *Server) handleSettingsDefaultSiteUpdate(w http.ResponseWriter, r *http.Request) {
	var req settings.UpdateDefaultSiteRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	requestID := middleware.GetRequestID(r.Context())
	result, err := s.settingsSvc.UpdateDefaultSite(r.Context(), &req, requestID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleSettingsHTTPSHijackGet(w http.ResponseWriter, r *http.Request) {
	h, err := s.settingsSvc.GetHTTPSHijack()
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	WriteOK(w, r, h)
}

func (s *Server) handleSettingsHTTPSHijackUpdate(w http.ResponseWriter, r *http.Request) {
	var req settings.UpdateHTTPSHijackRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	requestID := middleware.GetRequestID(r.Context())
	result, err := s.settingsSvc.UpdateHTTPSHijack(r.Context(), &req, requestID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleSettingsLogRotateGet(w http.ResponseWriter, r *http.Request) {
	result, err := s.settingsSvc.GetLogRotate(r.Context())
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleSettingsLogRotateUpdate(w http.ResponseWriter, r *http.Request) {
	var req settings.UpdateLogRotateRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	requestID := middleware.GetRequestID(r.Context())
	result, err := s.settingsSvc.UpdateLogRotate(r.Context(), &req, requestID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleSecuritySettingsGet(w http.ResponseWriter, r *http.Request) {
	result := s.settingsSvc.GetSecuritySettings()
	WriteOK(w, r, result)
}

func (s *Server) handleSecuritySettingsUpdate(w http.ResponseWriter, r *http.Request) {
	var req settings.UpdateSecuritySettingsRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.settingsSvc.UpdateSecuritySettings(r.Context(), &req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
