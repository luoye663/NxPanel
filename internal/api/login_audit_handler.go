package api

import (
	"net/http"
	"strconv"
)

func (s *Server) handleLoginAuditList(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}

	items, total, err := s.loginAuditRepo.List(page, pageSize)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}

	type listItem struct {
		ID              int    `json:"id"`
		Username        string `json:"username"`
		IP              string `json:"ip"`
		UserAgent       string `json:"user_agent"`
		Success         bool   `json:"success"`
		FailureReason   string `json:"failure_reason"`
		CaptchaVerified bool   `json:"captcha_verified"`
		TOTPUsed        bool   `json:"totp_used"`
		CreatedAt       string `json:"created_at"`
	}

	list := make([]listItem, 0, len(items))
	for _, a := range items {
		list = append(list, listItem{
			ID:              a.ID,
			Username:        a.Username,
			IP:              a.IP,
			UserAgent:       a.UserAgent,
			Success:         a.Success,
			FailureReason:   a.FailureReason,
			CaptchaVerified: a.CaptchaVerified,
			TOTPUsed:        a.TOTPUsed,
			CreatedAt:       a.CreatedAt,
		})
	}

	WriteOK(w, r, map[string]any{
		"items":     list,
		"page":      page,
		"page_size": pageSize,
		"total":     total,
	})
}

func (s *Server) handleLoginAuditClear(w http.ResponseWriter, r *http.Request) {
	if err := s.loginAuditRepo.DeleteAll(); err != nil {
		WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error(), nil)
		return
	}
	WriteOK(w, r, map[string]any{"ok": true})
}
