package api

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/sitebackup"
)

func (s *Server) handleSiteBackupList(w http.ResponseWriter, r *http.Request) {
	result, err := s.siteBackupSvc.List(chi.URLParam(r, "site_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"items": result})
}

func (s *Server) handleSiteBackupCreate(w http.ResponseWriter, r *http.Request) {
	var req sitebackup.CreateRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.siteBackupSvc.StartCreate(chi.URLParam(r, "site_id"), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteCreated(w, r, result)
}

func (s *Server) handleSiteBackupDownload(w http.ResponseWriter, r *http.Request) {
	resp, filename, err := s.siteBackupSvc.Download(r.Context(), chi.URLParam(r, "site_id"), chi.URLParam(r, "backup_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleSiteBackupRestore(w http.ResponseWriter, r *http.Request) {
	var req sitebackup.RestoreRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.siteBackupSvc.StartRestore(chi.URLParam(r, "site_id"), chi.URLParam(r, "backup_id"), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleSiteBackupDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.siteBackupSvc.Delete(r.Context(), chi.URLParam(r, "site_id"), chi.URLParam(r, "backup_id"), middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]any{"ok": true})
}

func (s *Server) handleSiteBackupTaskStream(w http.ResponseWriter, r *http.Request) {
	stream, err := s.siteBackupSvc.TaskStream(chi.URLParam(r, "task_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	ServeSSE(w, r, stream, 20*time.Second)
}

func (s *Server) handleSiteBackupScheduleGet(w http.ResponseWriter, r *http.Request) {
	result, err := s.siteBackupSvc.GetSchedule(chi.URLParam(r, "site_id"))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}

func (s *Server) handleSiteBackupScheduleSave(w http.ResponseWriter, r *http.Request) {
	var req sitebackup.SaveScheduleRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	result, err := s.siteBackupSvc.SaveSchedule(chi.URLParam(r, "site_id"), &req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, result)
}
