package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/acme"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
)

func (s *Server) handleACMEApply(w http.ResponseWriter, r *http.Request) {
	var req acme.ApplyRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	orderID, err := s.acmeSvc.ApplyCertificate(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"order_id": orderID,
	})
}

func (s *Server) handleACMEOrderLog(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "order_id 不能为空", nil)
		return
	}

	stream := s.acmeSvc.StreamLogs(orderID)
	if stream == nil {
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "订单不存在或日志已过期", nil)
		return
	}

	ServeSSE(w, r, stream, app.ParseDurationOrDefault(s.cfg.API.SSEHeartbeat, 15*time.Second))
}

func (s *Server) handleACMEOrderList(w http.ResponseWriter, r *http.Request) {
	siteID := chi.URLParam(r, "site_id")
	if siteID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "site_id 不能为空", nil)
		return
	}

	orders, err := s.acmeSvc.ListOrders(siteID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, orders)
}

func (s *Server) handleACMEOrderRenew(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "order_id 不能为空", nil)
		return
	}

	newOrderID, err := s.acmeSvc.RenewOrder(r.Context(), orderID, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"order_id": newOrderID,
	})
}

func (s *Server) handleACMEOrderDelete(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "order_id 不能为空", nil)
		return
	}

	if err := s.acmeSvc.DeleteOrder(r.Context(), orderID, middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"operation_id": "",
	})
}

func (s *Server) handleACMEOrderDownload(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "order_id 不能为空", nil)
		return
	}

	zipBytes, filename, err := s.acmeSvc.DownloadOrder(r.Context(), orderID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.Write(zipBytes)
}

func (s *Server) handleACMEOrderDeploy(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "order_id 不能为空", nil)
		return
	}

	if err := s.acmeSvc.DeployOrder(r.Context(), orderID, middleware.GetRequestID(r.Context())); err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"message": "证书已部署",
	})
}

func (s *Server) handleACMEOrderAutoRenew(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "order_id 不能为空", nil)
		return
	}

	var req struct {
		AutoRenew bool `json:"auto_renew"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}

	if err := s.acmeSvc.SetAutoRenew(orderID, req.AutoRenew); err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"auto_renew": req.AutoRenew,
	})
}

func (s *Server) handleACMEOrderForceObtain(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if orderID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "order_id 不能为空", nil)
		return
	}

	newOrderID, err := s.acmeSvc.ForceObtain(orderID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"order_id": newOrderID,
	})
}

func (s *Server) handleACMEEmailList(w http.ResponseWriter, r *http.Request) {
	emails, err := s.acmeSvc.ListEmails()
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, emails)
}

func (s *Server) handleACMEEmailSave(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if !DecodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "email 不能为空", nil)
		return
	}

	if err := s.acmeSvc.SaveEmail(req.Email); err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, nil)
}

func (s *Server) handleACMEEmailDelete(w http.ResponseWriter, r *http.Request) {
	email := chi.URLParam(r, "email")
	if email == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "email 不能为空", nil)
		return
	}

	decoded, err := url.PathUnescape(email)
	if err != nil {
		decoded = email
	}

	if err := s.acmeSvc.DeleteEmail(decoded); err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, nil)
}
