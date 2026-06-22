// api 包 — operation handler
//
// 处理操作记录接口：
//   - GET    /api/v1/operations           操作记录列表
//   - GET    /api/v1/operations/{id}      操作详情
//   - DELETE /api/v1/operations           清空操作记录
package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// handleOperationList 操作记录列表
func (s *Server) handleOperationList(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	targetType := r.URL.Query().Get("target_type")
	targetID := r.URL.Query().Get("target_id")

	ops, total, err := s.opRepo.List(page, pageSize, targetType, targetID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	type listItem struct {
		ID         string  `json:"id"`
		Action     string  `json:"action"`
		TargetType string  `json:"target_type"`
		TargetID   string  `json:"target_id"`
		Status     string  `json:"status"`
		Message    string  `json:"message"`
		CreatedAt  string  `json:"created_at"`
		FinishedAt *string `json:"finished_at"`
	}

	items := make([]listItem, 0, len(ops))
	for _, o := range ops {
		items = append(items, listItem{
			ID:         o.ID,
			Action:     o.Action,
			TargetType: o.TargetType,
			TargetID:   o.TargetID,
			Status:     o.Status,
			Message:    o.Message,
			CreatedAt:  o.CreatedAt,
			FinishedAt: o.FinishedAt,
		})
	}

	WriteOK(w, r, map[string]any{
		"items":     items,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	})
}

// handleOperationDetail 操作详情
func (s *Server) handleOperationDetail(w http.ResponseWriter, r *http.Request) {
	opID := chi.URLParam(r, "operation_id")
	if opID == "" {
		WriteError(w, r, http.StatusBadRequest, "BAD_REQUEST", "operation_id 不能为空", nil)
		return
	}

	op, err := s.opRepo.GetByID(opID)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	if op == nil {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "操作记录不存在", nil)
		return
	}

	// 查询关联的备份记录
	backups, _ := s.backupRepo.ListByOperationID(opID)

	type backupItem struct {
		FilePath   string `json:"file_path"`
		BackupPath string `json:"backup_path"`
	}

	backupItems := make([]backupItem, 0, len(backups))
	for _, b := range backups {
		backupItems = append(backupItems, backupItem{
			FilePath:   b.FilePath,
			BackupPath: b.BackupPath,
		})
	}

	WriteOK(w, r, map[string]any{
		"id":            op.ID,
		"action":        op.Action,
		"target_type":   op.TargetType,
		"target_id":     op.TargetID,
		"status":        op.Status,
		"request_id":    op.RequestID,
		"actor":         op.Actor,
		"message":       op.Message,
		"error_code":    op.ErrorCode,
		"error_message": op.ErrorMessage,
		"stderr":        op.Stderr,
		"created_at":    op.CreatedAt,
		"finished_at":   op.FinishedAt,
		"backups":       backupItems,
	})
}

func (s *Server) handleOperationClear(w http.ResponseWriter, r *http.Request) {
	if err := s.opRepo.DeleteAll(); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	WriteOK(w, r, map[string]any{"ok": true})
}
