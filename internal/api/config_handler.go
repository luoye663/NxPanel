package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/config"
)

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	result, err := s.configSvc.Get(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req config.SaveRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.configSvc.Save(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleConfigMigrateMarkers(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	result, err := s.configSvc.MigrateMarkers(r.Context(), siteID, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}
