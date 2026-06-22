// api 包 — SSL handler
//
// 处理 SSL 证书管理接口：
//   - GET    /api/v1/sites/{site_id}/ssl                获取 SSL 状态
//   - PUT    /api/v1/sites/{site_id}/ssl/manual-pem     上传 PEM 证书
//   - PUT    /api/v1/sites/{site_id}/ssl/existing-files 使用已有证书路径
//   - DELETE /api/v1/sites/{site_id}/ssl                禁用 SSL
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/ssl"
)

// handleSSLGet 获取站点 SSL 状态
func (s *Server) handleSSLGet(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	result, err := s.sslSvc.Get(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, result)
}

// handleSSLManualPEM 上传 PEM 证书并启用 SSL
func (s *Server) handleSSLManualPEM(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req ssl.ManualPEMRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, opID, err := s.sslSvc.ManualPEM(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	// 对应文档 7.5.2 节返回格式
	WriteOK(w, r, map[string]any{
		"ssl": map[string]any{
			"enabled":   result.Enabled,
			"mode":      result.Mode,
			"not_after": result.NotAfter,
			"dns_names": result.DNSNames,
		},
		"operation_id": opID,
	})
}

// handleSSLExistingFiles 使用已有证书路径启用 SSL
func (s *Server) handleSSLExistingFiles(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req ssl.ExistingFilesRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, opID, err := s.sslSvc.ExistingFiles(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	// 对应文档 7.5.3 节返回格式
	WriteOK(w, r, map[string]any{
		"ssl": map[string]any{
			"enabled":   result.Enabled,
			"mode":      result.Mode,
			"cert_path": result.CertPath,
			"not_after": result.NotAfter,
		},
		"operation_id": opID,
	})
}

// handleSSLDisable 禁用 SSL
func (s *Server) handleSSLDisable(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req ssl.DisableSSLRequest
	if !DecodeJSONOptional(w, r, &req) {
		return
	}

	result, opID, err := s.sslSvc.Disable(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	// 对应文档 7.5.4 节返回格式
	WriteOK(w, r, map[string]any{
		"ssl": map[string]any{
			"enabled": result.Enabled,
			"mode":    result.Mode,
		},
		"operation_id": opID,
	})
}

func (s *Server) handleSSLContent(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	certPEM, keyPEM, err := s.sslSvc.GetContent(r.Context(), siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"certificate_pem": certPEM,
		"private_key_pem": keyPEM,
	})
}

func (s *Server) handleSSLDownload(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	zipBytes, filename, err := s.sslSvc.DownloadZIP(r.Context(), siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Write(zipBytes)
}
