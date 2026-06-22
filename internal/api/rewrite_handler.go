// api 包 — rewrite handler
//
// 处理自定义 Location 接口：
//   - GET /api/v1/sites/{site_id}/rewrite  获取自定义 Location 内容
//   - PUT /api/v1/sites/{site_id}/rewrite  保存自定义 Location 内容
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/rewrite"
)

// handleRewriteGet 获取自定义 Location 内容
func (s *Server) handleRewriteGet(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	result, err := s.rewriteSvc.Get(r.Context(), siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

// handleRewriteUpdate 保存自定义 Location 内容
func (s *Server) handleRewriteUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req rewrite.UpdateRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.rewriteSvc.Update(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

func (s *Server) handleRewriteTemplateList(w http.ResponseWriter, r *http.Request) {
	result, err := s.rewriteSvc.ListTemplates()
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleRewriteTemplateCreate(w http.ResponseWriter, r *http.Request) {
	var req rewrite.TemplateInput
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.rewriteSvc.CreateTemplate(&req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleRewriteTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "template_id")
	if templateID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "template_id 不能为空", nil)
		return
	}
	var req rewrite.TemplateInput
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.rewriteSvc.UpdateTemplate(templateID, &req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleRewriteTemplateDelete(w http.ResponseWriter, r *http.Request) {
	templateID := chi.URLParam(r, "template_id")
	if templateID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "template_id 不能为空", nil)
		return
	}
	if err := s.rewriteSvc.DeleteTemplate(templateID); err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"deleted": true})
}

func (s *Server) handleRewriteTemplatePreview(w http.ResponseWriter, r *http.Request) {
	var req rewrite.TemplatePreviewRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.rewriteSvc.PreviewTemplate(&req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleRewriteApplyTemplate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}
	var req rewrite.ApplyTemplateRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.rewriteSvc.ApplyTemplate(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
