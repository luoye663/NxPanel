package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/accessanalysis"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
)

func (s *Server) handleAccessAnalysisSummary(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	result, err := s.accessAnalysisSvc.Summary(r.Context(), chi.URLParam(r, "site_id"), parseAccessAnalysisQuery(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisScan(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	var req accessanalysis.ScanRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.accessAnalysisSvc.Scan(r.Context(), chi.URLParam(r, "site_id"), req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisJobs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	page, pageSize := parsePage(r)
	result, err := s.accessAnalysisSvc.Jobs(chi.URLParam(r, "site_id"), page, pageSize)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisPaths(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	result, err := s.accessAnalysisSvc.Paths(chi.URLParam(r, "site_id"), parseAccessAnalysisQuery(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisIPs(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	result, err := s.accessAnalysisSvc.IPs(chi.URLParam(r, "site_id"), parseAccessAnalysisQuery(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisEntries(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	result, err := s.accessAnalysisSvc.Entries(chi.URLParam(r, "site_id"), parseAccessAnalysisQuery(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisAnomalies(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	result, err := s.accessAnalysisSvc.Anomalies(chi.URLParam(r, "site_id"), parseAccessAnalysisQuery(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisExport(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	kind := r.URL.Query().Get("kind")
	filename := fmt.Sprintf("access-analysis-%s-%s.csv", kind, time.Now().UTC().Format("20060102"))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	if err := s.accessAnalysisSvc.ExportCSV(w, kind, chi.URLParam(r, "site_id"), parseAccessAnalysisQuery(r)); err != nil {
		// CSV 响应头可能已经写出，因此这里只记录错误到响应体末尾，避免混用 JSON。
		_, _ = w.Write([]byte("\nexport_error," + err.Error() + "\n"))
	}
}

func (s *Server) handleAccessAnalysisSettingsGet(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	result, err := s.accessAnalysisSvc.Settings(chi.URLParam(r, "site_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisSettingsPut(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	var req accessanalysis.Settings
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.accessAnalysisSvc.SaveSettings(chi.URLParam(r, "site_id"), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisFormatDetect(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	var req accessanalysis.FormatDetectRequest
	if !DecodeJSONOptional(w, r, &req) {
		return
	}
	result, err := s.accessAnalysisSvc.DetectFormat(r.Context(), chi.URLParam(r, "site_id"), req.Sample)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisFormatTest(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	var req accessanalysis.FormatTestRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.accessAnalysisSvc.TestFormat(req.Pattern, req.Sample)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleAccessAnalysisFormatOptimize(w http.ResponseWriter, r *http.Request) {
	if !s.requireAccessAnalysis(w, r) {
		return
	}
	result, err := s.accessAnalysisSvc.OptimizeFormat(chi.URLParam(r, "site_id"), middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) requireAccessAnalysis(w http.ResponseWriter, r *http.Request) bool {
	if s.accessAnalysisSvc != nil {
		return true
	}
	WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "访问分析需要 Agent 可用", nil)
	return false
}

func parseAccessAnalysisQuery(r *http.Request) accessanalysis.Query {
	page, pageSize := parsePage(r)
	status, _ := strconv.Atoi(r.URL.Query().Get("status"))
	return accessanalysis.Query{From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to"), IP: r.URL.Query().Get("ip"), Path: r.URL.Query().Get("path"), Method: r.URL.Query().Get("method"), Status: status, Sort: r.URL.Query().Get("sort"), Page: page, PageSize: pageSize}
}

func parsePage(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	return page, pageSize
}
