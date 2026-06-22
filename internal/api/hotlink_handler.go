package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/hotlink"
)

func (s *Server) handleHotlinkRuleList(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}
	result, err := s.hotlinkSvc.List(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleHotlinkRuleCreate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}
	var req hotlink.SaveRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.hotlinkSvc.Create(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteCreated(w, r, result)
}

func (s *Server) handleHotlinkRuleUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}
	var req hotlink.SaveRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.hotlinkSvc.Update(r.Context(), siteID, ruleID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleHotlinkRuleDelete(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}
	if err := s.hotlinkSvc.Delete(r.Context(), siteID, ruleID, middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"deleted": true})
}
