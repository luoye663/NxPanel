package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/upstream"
)

func (s *Server) requireUpstreamService(w http.ResponseWriter, r *http.Request) bool {
	if s.upstreamSvc != nil {
		return true
	}
	WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "upstream service unavailable", nil)
	return false
}

func (s *Server) handleUpstreamList(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	result, err := s.upstreamSvc.List(r.Context())
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleUpstreamGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	result, err := s.upstreamSvc.Get(r.Context(), chi.URLParam(r, "upstream_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleUpstreamStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	result, err := s.upstreamSvc.Status(r.Context())
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleUpstreamValidate(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	var req upstream.SaveRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.upstreamSvc.Validate(r.Context(), &req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleUpstreamCreate(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	var req upstream.SaveRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.upstreamSvc.Create(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteCreated(w, r, result)
}

func (s *Server) handleUpstreamUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	var req upstream.SaveRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.upstreamSvc.Update(r.Context(), chi.URLParam(r, "upstream_id"), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleUpstreamDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	result, err := s.upstreamSvc.Delete(r.Context(), chi.URLParam(r, "upstream_id"), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleUpstreamSync(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpstreamService(w, r) {
		return
	}
	var req struct{}
	if !DecodeJSONOptional(w, r, &req) {
		return
	}
	result, err := s.upstreamSvc.Sync(r.Context(), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
