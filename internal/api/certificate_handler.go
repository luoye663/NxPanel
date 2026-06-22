package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/ssl"
)

func (s *Server) handleCertificateList(w http.ResponseWriter, r *http.Request) {
	certs, err := s.sslSvc.ListStoreCertificates()
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	WriteOK(w, r, certs)
}

func (s *Server) handleCertificateCreate(w http.ResponseWriter, r *http.Request) {
	var req ssl.UploadToStoreRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, opID, err := s.sslSvc.UploadToStore(r.Context(), &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"certificate": result,
		"operation_id": opID,
	})
}

func (s *Server) handleCertificateDelete(w http.ResponseWriter, r *http.Request) {
	certID := chi.URLParam(r, "cert_id")
	if certID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "cert_id 不能为空", nil)
		return
	}

	opID, err := s.sslSvc.DeleteFromStore(r.Context(), certID, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"operation_id": opID,
	})
}

func (s *Server) handleCertificateDeploy(w http.ResponseWriter, r *http.Request) {
	certID := chi.URLParam(r, "cert_id")
	if certID == "" {
		WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "cert_id 不能为空", nil)
		return
	}

	var req ssl.DeployFromStoreRequest
	if !DecodeJSON(w, r, &req) {
		return
	}

	result, opID, err := s.sslSvc.DeployFromStore(r.Context(), certID, &req, middleware.GetRequestID(r.Context()))
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	WriteOK(w, r, map[string]any{
		"ssl": map[string]any{
			"enabled":   result.Enabled,
			"mode":      result.Mode,
			"cert_path": result.CertPath,
			"not_after": result.NotAfter,
			"dns_names": result.DNSNames,
		},
		"operation_id": opID,
	})
}
