package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/luoye663/nxpanel/internal/accesspolicy"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
)

func (s *Server) requireAccessPolicy(w http.ResponseWriter, r *http.Request) bool {
	if s.accessPolicySvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "访问规则服务不可用", nil)
		return false
	}
	return true
}

func (s *Server) handleAccessPolicyGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessPolicy(w, r) {
		return
	}
	p, err := s.accessPolicySvc.Get(chi.URLParam(r, "site_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, p)
}
func (s *Server) handleAccessPolicySave(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessPolicy(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var p accesspolicy.Policy
	if !DecodeJSON(w, r, &p) {
		return
	}
	result, err := s.accessPolicySvc.Save(r.Context(), chi.URLParam(r, "site_id"), p, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleAccessPolicyPreview(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessPolicy(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var req struct {
		Policy  accesspolicy.Policy         `json:"policy"`
		Request accesspolicy.PreviewRequest `json:"request"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.accessPolicySvc.Preview(chi.URLParam(r, "site_id"), req.Policy, req.Request)
	if err != nil {
		writeAppError(w, r, app.ErrValidationFailedMsg(err.Error(), nil))
		return
	}
	WriteOK(w, r, result)
}
func (s *Server) handleAccessPolicySync(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessPolicy(w, r) {
		return
	}
	result, err := s.accessPolicySvc.Sync(r.Context(), chi.URLParam(r, "site_id"), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) guardLegacyAccess(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.accessPolicySvc != nil {
			release, err := s.accessPolicySvc.LegacyLease(chi.URLParam(r, "site_id"))
			if err != nil {
				writeAppError(w, r, err)
				return
			}
			defer release()
		}
		next(w, r)
	}
}
