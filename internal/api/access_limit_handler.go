package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/accesslimit"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
)

func (s *Server) handleAuthRuleList(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	result, err := s.accessLimitSvc.ListAuthRules(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleAuthRuleCreate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req accesslimit.CreateAuthRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.accessLimitSvc.CreateAuthRule(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteCreated(w, r, result)
}

func (s *Server) handleAuthRuleUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}

	var req accesslimit.UpdateAuthRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.accessLimitSvc.UpdateAuthRule(r.Context(), siteID, ruleID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleAuthRuleDelete(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}

	if err := s.accessLimitSvc.DeleteAuthRule(r.Context(), siteID, ruleID, middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{"deleted": true})
}

func (s *Server) handleDenyRuleList(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	result, err := s.accessLimitSvc.ListDenyRules(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleDenyRuleCreate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req accesslimit.CreateDenyRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.accessLimitSvc.CreateDenyRule(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteCreated(w, r, result)
}

func (s *Server) handleDenyRuleUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}

	var req accesslimit.UpdateDenyRuleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.accessLimitSvc.UpdateDenyRule(r.Context(), siteID, ruleID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleDenyRuleDelete(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}

	if err := s.accessLimitSvc.DeleteDenyRule(r.Context(), siteID, ruleID, middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{"deleted": true})
}

func (s *Server) handleIPLimitRuleList(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}
	result, err := s.accessLimitSvc.ListIPLimitRules(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleIPLimitRuleCreate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}
	var req struct {
		Name     string `json:"name"`
		RuleType string `json:"rule_type"`
		IPsText  string `json:"ips_text"`
		Enabled  *bool  `json:"enabled"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.accessLimitSvc.CreateIPLimitRule(r.Context(), siteID, &accesslimit.CreateIPLimitRuleRequest{
		Name:     strings.TrimSpace(req.Name),
		RuleType: strings.TrimSpace(req.RuleType),
		IPs:      splitIPsText(req.IPsText),
		Enabled:  req.Enabled,
	}, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteCreated(w, r, result)
}

func (s *Server) handleIPLimitRuleUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}
	var req struct {
		Name     string `json:"name"`
		RuleType string `json:"rule_type"`
		IPsText  string `json:"ips_text"`
		Enabled  *bool  `json:"enabled"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.accessLimitSvc.UpdateIPLimitRule(r.Context(), siteID, ruleID, &accesslimit.UpdateIPLimitRuleRequest{
		Name:     strings.TrimSpace(req.Name),
		RuleType: strings.TrimSpace(req.RuleType),
		IPs:      splitIPsText(req.IPsText),
		Enabled:  req.Enabled,
	}, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleIPLimitRuleDelete(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	ruleID := chi.URLParam(r, "rule_id")
	if siteID == "" || ruleID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "参数不能为空", nil)
		return
	}
	if err := s.accessLimitSvc.DeleteIPLimitRule(r.Context(), siteID, ruleID, middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"deleted": true})
}

func splitIPsText(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ',' || r == ';' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
