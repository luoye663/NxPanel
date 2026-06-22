package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

func (s *Server) handleScheduledTaskDefinitions(w http.ResponseWriter, r *http.Request) {
	if s.scheduledTaskSvc == nil {
		WriteError(w, r, http.StatusServiceUnavailable, app.ErrAgentUnavailable, "计划任务中心未初始化", nil)
		return
	}
	WriteOK(w, r, scheduledtask.TaskDefinitionResponse{Items: s.scheduledTaskSvc.Definitions()})
}

func (s *Server) handleScheduledTaskList(w http.ResponseWriter, r *http.Request) {
	items, err := s.scheduledTaskSvc.List(r.Context())
	if err != nil {
		writeScheduledTaskError(w, r, err)
		return
	}
	WriteOK(w, r, scheduledtask.TaskListResponse{Items: items})
}

func (s *Server) handleScheduledTaskCreate(w http.ResponseWriter, r *http.Request) {
	var req scheduledtask.CreateTaskRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	item, err := s.scheduledTaskSvc.Create(r.Context(), req)
	if err != nil {
		writeScheduledTaskError(w, r, err)
		return
	}
	WriteCreated(w, r, item)
}

func (s *Server) handleScheduledTaskUpdate(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	if taskID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "task_id 不能为空", nil)
		return
	}
	var req scheduledtask.UpdateTaskRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	item, err := s.scheduledTaskSvc.Update(r.Context(), taskID, req)
	if err != nil {
		writeScheduledTaskError(w, r, err)
		return
	}
	WriteOK(w, r, item)
}

func (s *Server) handleScheduledTaskToggle(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	var req scheduledtask.ToggleTaskRequest
	if !DecodeJSON(w, r, &req) {
		return
	}
	if err := s.scheduledTaskSvc.SetEnabled(r.Context(), taskID, req.Enabled); err != nil {
		writeScheduledTaskError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]bool{"ok": true})
}

func (s *Server) handleScheduledTaskRunNow(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	if err := s.scheduledTaskSvc.RunNow(r.Context(), taskID); err != nil {
		writeScheduledTaskError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]bool{"queued": true})
}

func (s *Server) handleScheduledTaskRunList(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := s.scheduledTaskSvc.ListRuns(r.Context(), taskID, limit)
	if err != nil {
		writeScheduledTaskError(w, r, err)
		return
	}
	WriteOK(w, r, scheduledtask.RunListResponse{Items: items})
}

func (s *Server) handleScheduledTaskDelete(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "task_id")
	if err := s.scheduledTaskSvc.Delete(r.Context(), taskID); err != nil {
		writeScheduledTaskError(w, r, err)
		return
	}
	WriteOK(w, r, map[string]bool{"ok": true})
}

func writeScheduledTaskError(w http.ResponseWriter, r *http.Request, err error) {
	var appErr *app.AppError
	if errors.As(err, &appErr) {
		status := http.StatusBadRequest
		switch appErr.Code {
		case app.ErrNotFound:
			status = http.StatusNotFound
		case app.ErrConflict:
			status = http.StatusConflict
		case app.ErrForbidden:
			status = http.StatusForbidden
		}
		WriteError(w, r, status, appErr.Code, appErr.Message, appErr.Details)
		return
	}
	WriteError(w, r, http.StatusInternalServerError, app.ErrInternalError, err.Error(), nil)
}
