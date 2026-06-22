// api 包 — proxy handler
//
// 处理反向代理配置接口：
//   - GET    /api/v1/sites/{site_id}/proxy              列出所有代理
//   - POST   /api/v1/sites/{site_id}/proxy              创建代理
//   - GET    /api/v1/sites/{site_id}/proxy/{proxy_id}   获取单个代理
//   - PUT    /api/v1/sites/{site_id}/proxy/{proxy_id}   更新代理
//   - DELETE /api/v1/sites/{site_id}/proxy/{proxy_id}   删除代理
//
// handler 层只做 HTTP 参数解析和响应格式化，业务逻辑在 proxy.Service 中。
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/proxy"
)

// ============================================================
// GET — 列出所有代理
// ============================================================

// handleProxyList 列出站点的所有反向代理配置
func (s *Server) handleProxyList(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "site_id 不能为空", nil)
		return
	}

	result, err := s.proxySvc.List(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

// ============================================================
// POST — 创建代理
// ============================================================

// handleProxyCreate 创建反向代理配置
func (s *Server) handleProxyCreate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "site_id 不能为空", nil)
		return
	}

	var req proxy.CreateProxyRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, opID, err := s.proxySvc.Create(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"proxy":        result,
		"operation_id": opID,
	})
}

// ============================================================
// GET — 获取单个代理
// ============================================================

// handleProxyGet 获取单个反向代理配置
func (s *Server) handleProxyGet(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	proxyID := chi.URLParam(r, "proxy_id")
	if siteID == "" || proxyID == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "site_id 和 proxy_id 不能为空", nil)
		return
	}

	result, err := s.proxySvc.Get(siteID, proxyID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

// ============================================================
// PUT — 更新代理
// ============================================================

// handleProxyUpdate 更新反向代理配置
func (s *Server) handleProxyUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	proxyID := chi.URLParam(r, "proxy_id")
	if siteID == "" || proxyID == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "site_id 和 proxy_id 不能为空", nil)
		return
	}

	var req proxy.UpdateProxyRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, opID, err := s.proxySvc.Update(r.Context(), siteID, proxyID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"proxy":        result,
		"operation_id": opID,
	})
}

// ============================================================
// DELETE — 删除代理
// ============================================================

// handleProxyDelete 删除反向代理配置
func (s *Server) handleProxyDelete(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	proxyID := chi.URLParam(r, "proxy_id")
	if siteID == "" || proxyID == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "site_id 和 proxy_id 不能为空", nil)
		return
	}

	opID, err := s.proxySvc.Delete(r.Context(), siteID, proxyID, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"operation_id": opID,
	})
}
