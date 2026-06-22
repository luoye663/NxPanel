// api 包 — site handler
//
// 处理站点 CRUD 接口：
//   - GET    /api/v1/sites                   站点列表
//   - POST   /api/v1/sites                   创建站点
//   - GET    /api/v1/sites/{site_id}         站点详情
//   - PUT    /api/v1/sites/{site_id}         修改基础配置
//   - DELETE /api/v1/sites/{site_id}         删除站点
//   - POST   /api/v1/sites/{site_id}/enable  启用站点
//   - POST   /api/v1/sites/{site_id}/disable 禁用站点
//
// handler 层只做 HTTP 参数解析和响应格式化，业务逻辑在 sites.Service 中。
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/sites"
)

// ============================================================
// List — 站点列表
// ============================================================

// siteListQuery 站点列表查询参数
type siteListQuery struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Keyword  string `json:"keyword"`
	Status   string `json:"status"`
}

// handleSiteList 获取站点列表
func (s *Server) handleSiteList(w http.ResponseWriter, r *http.Request) {
	q := parseSiteListQuery(r)

	siteList, total, err := s.siteSvc.List(q.Page, q.PageSize, q.Keyword, q.Status)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
		return
	}

	items := make([]sites.SiteListItem, 0, len(siteList))

	siteIDs := make([]string, 0, len(siteList))
	for _, site := range siteList {
		siteIDs = append(siteIDs, site.ID)
	}

	sslStatus, _ := s.sslRepo.EnabledBySiteIDs(siteIDs)
	proxyStatus, _ := s.proxyRepo.EnabledBySiteIDs(siteIDs)

	for _, site := range siteList {
		var domains []string
		json.Unmarshal([]byte(site.DomainsJSON), &domains)

		var bindings []sites.Binding
		if site.BindingsJSON != "" {
			json.Unmarshal([]byte(site.BindingsJSON), &bindings)
		}
		if len(bindings) == 0 {
			for _, d := range domains {
				bindings = append(bindings, sites.Binding{Domain: d, Port: site.HTTPPort})
			}
		}

		items = append(items, sites.SiteListItem{
			ID:            site.ID,
			PrimaryDomain: site.PrimaryDomain,
			Domains:       domains,
			Bindings:      bindings,
			Status:        site.Status,
			RootPath:      site.RootPath,
			AccessLogPath: site.AccessLogPath,
			ErrorLogPath:  site.ErrorLogPath,
			UpdatedAt:     site.UpdatedAt,
			SSLEnabled:    sslStatus[site.ID],
			ProxyEnabled:  proxyStatus[site.ID],
		})
	}

	WriteOK(w, r, map[string]any{
		"items":     items,
		"total":     total,
		"page":      q.Page,
		"page_size": q.PageSize,
	})
}

// parseSiteListQuery 从 URL query 中解析列表参数
func parseSiteListQuery(r *http.Request) *siteListQuery {
	q := &siteListQuery{
		Page:     1,
		PageSize: 20,
	}
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			q.Page = n
		}
	}
	if v := r.URL.Query().Get("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			q.PageSize = n
		}
	}
	q.Keyword = strings.TrimSpace(r.URL.Query().Get("keyword"))
	q.Status = strings.TrimSpace(r.URL.Query().Get("status"))
	return q
}

// ============================================================
// Create — 创建站点
// ============================================================

// handleSiteCreate 创建网站
func (s *Server) handleSiteCreate(w http.ResponseWriter, r *http.Request) {
	var req sites.CreateSiteRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	site, opID, err := s.siteSvc.Create(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteCreated(w, r, map[string]any{
		"site_id":      site.ID,
		"operation_id": opID,
		"status":       site.Status,
	})
}

// ============================================================
// Detail — 站点详情
// ============================================================

// handleSiteDetail 获取站点详情
func (s *Server) handleSiteDetail(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	site, proxy, ssl, err := s.siteSvc.Get(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	// For imported sites, lazy-refresh log paths from the actual config file.
	// This keeps the DB in sync when users edit config files externally.
	if site.ConfigPath == site.EnabledPath {
		site, _ = s.siteSvc.RefreshLogPaths(r.Context(), site)
	}

	var domains []string
	json.Unmarshal([]byte(site.DomainsJSON), &domains)

	var bindings []sites.Binding
	if site.BindingsJSON != "" {
		json.Unmarshal([]byte(site.BindingsJSON), &bindings)
	}
	if len(bindings) == 0 {
		for _, d := range domains {
			bindings = append(bindings, sites.Binding{Domain: d, Port: site.HTTPPort})
		}
	}

	detail := sites.SiteDetailResponse{
		ID:                 site.ID,
		PrimaryDomain:      site.PrimaryDomain,
		Domains:            domains,
		Bindings:           bindings,
		Status:             site.Status,
		HTTPPort:           site.HTTPPort,
		HTTPSPort:          site.HTTPSPort,
		ConfigPath:         site.ConfigPath,
		RootPath:           site.RootPath,
		IndexFiles:         site.IndexFiles,
		IndexFileList:      strings.Fields(site.IndexFiles),
		AutoindexEnabled:   site.AutoindexEnabled,
		AutoindexExactSize: site.AutoindexExactSize,
		AutoindexLocaltime: site.AutoindexLocaltime,
		AutoindexFormat:    site.AutoindexFormat,
		ErrorPage404:       site.ErrorPage404,
		ErrorPage403:       site.ErrorPage403,
		AccessLogEnabled:   site.AccessLogEnabled,
		AccessLogPath:      site.AccessLogPath,
		ErrorLogPath:       site.ErrorLogPath,
		IsImported:         site.ConfigPath == site.EnabledPath,
		ImportWarnings:     s.siteSvc.ImportWarnings(r.Context(), site),
		Proxy: &sites.ProxyBrief{
			Enabled: proxy != nil && proxy.Enabled,
		},
		SSL: &sites.SSLBrief{
			Enabled: ssl != nil && ssl.Enabled,
		},
	}
	if s.agentClient != nil {
		// 详情页需要知道 marker 是否完整，用于禁用会修改 Nginx 片段的表单。
		if content, _, readErr := s.agentClient.ReadFile(r.Context(), site.ConfigPath); readErr == nil {
			detail.MarkerStatus = nginx.ValidateRequiredMarkers(content, nginx.RequiredSiteMarkers())
		}
	}

	WriteOK(w, r, detail)
}

// ============================================================
// Update — 修改基础配置
// ============================================================

// handleSiteUpdate 修改站点基础配置
func (s *Server) handleSiteUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req sites.UpdateSiteRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	site, opID, err := s.siteSvc.Update(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"site_id":      site.ID,
		"operation_id": opID,
	})
}

func (s *Server) handleSiteDocumentUpdate(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req sites.UpdateSiteDocumentRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	site, opID, err := s.siteSvc.UpdateDocument(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{"site_id": site.ID, "operation_id": opID})
}

// ============================================================
// Enable — 启用站点
// ============================================================

// handleSiteEnable 启用站点
func (s *Server) handleSiteEnable(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req sites.EnableSiteRequest
	if !DecodeJSONOptional(w, r, &req) {
		return
	}

	status, opID, err := s.siteSvc.Enable(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"status":       status,
		"operation_id": opID,
	})
}

// ============================================================
// Disable — 禁用站点
// ============================================================

// handleSiteDisable 禁用站点
func (s *Server) handleSiteDisable(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req sites.DisableSiteRequest
	if !DecodeJSONOptional(w, r, &req) {
		return
	}

	status, opID, err := s.siteSvc.Disable(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"status":       status,
		"operation_id": opID,
	})
}

// ============================================================
// Delete — 删除站点
// ============================================================

// handleSiteDelete 删除站点
func (s *Server) handleSiteDelete(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	var req sites.DeleteSiteRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	ok, opID, err := s.siteSvc.Delete(r.Context(), siteID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"deleted":      ok,
		"operation_id": opID,
	})
}

// ============================================================
// 辅助
// ============================================================

// writeAppError 将 sites.Service 返回的 AppError 转为 HTTP 响应
func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	if appErr, ok := err.(*app.AppError); ok {
		status := appErrorToHTTPStatus(appErr.Code)
		WriteError(w, r, status, appErr.Code, appErr.Message, appErr.Details)
		return
	}
	WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
}

// ============================================================
// Import Scan — 扫描可导入的旧站点
// ============================================================

func (s *Server) handleImportScan(w http.ResponseWriter, r *http.Request) {
	result, err := s.siteSvc.ImportScan(r.Context(), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

// ============================================================
// Import — 导入旧站点
// ============================================================

func (s *Server) handleSiteImport(w http.ResponseWriter, r *http.Request) {
	var req sites.ImportSiteRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, err := s.siteSvc.Import(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteCreated(w, r, result)
}

// appErrorToHTTPStatus 将错误码映射为 HTTP 状态码
func appErrorToHTTPStatus(code string) int {
	switch code {
	case app.ErrBadRequest:
		return http.StatusBadRequest
	case app.ErrUnauthorized:
		return http.StatusUnauthorized
	case app.ErrForbidden:
		return http.StatusForbidden
	case app.ErrNotFound:
		return http.StatusNotFound
	case app.ErrConflict:
		return http.StatusConflict
	case app.ErrValidationFailed:
		return http.StatusUnprocessableEntity
	case app.ErrConfigDrifted:
		return http.StatusConflict
	case app.ErrNginxTestFailed:
		return http.StatusUnprocessableEntity
	case app.ErrNginxReloadFailed:
		return http.StatusInternalServerError
	case app.ErrAgentUnavailable:
		return http.StatusServiceUnavailable
	case app.ErrAgentDenied:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
