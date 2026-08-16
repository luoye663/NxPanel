package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/upload"
)

func (s *Server) receiveUpload(w http.ResponseWriter, r *http.Request) (*upload.File, string, bool) {
	maxBytes := app.ParseSizeOrDefault(s.cfg.API.MaxUploadSize, 100*1024*1024)
	if maxBytes <= 0 {
		maxBytes = 100 * 1024 * 1024
	}
	file, err := upload.Parse(r, maxBytes)
	if err != nil {
		writeUploadRequestError(w, r, err)
		return nil, "", false
	}
	targetPath := file.Path
	if strings.HasSuffix(targetPath, "/") {
		targetPath += file.Filename
	}
	return file, targetPath, true
}

func (s *Server) streamUploadToAgent(r *http.Request, targetPath string, file *upload.File) error {
	timeout := app.ParseDurationOrDefault(s.cfg.API.UploadTimeout, 300*time.Second)
	return s.agentClient.FilesUploadStream(r.Context(), targetPath, file.File, timeout)
}

func writeUploadRequestError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, upload.ErrTooLarge) {
		WriteError(w, r, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "上传文件超过大小限制", nil)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(r.Context().Err(), context.DeadlineExceeded) || isTimeoutError(err) {
		WriteError(w, r, http.StatusRequestTimeout, "UPLOAD_TIMEOUT", "上传读取超时", nil)
		return
	}
	WriteError(w, r, http.StatusBadRequest, app.ErrBadRequest, "上传请求格式错误", nil)
}

func writeUploadAgentError(w http.ResponseWriter, r *http.Request, err error) bool {
	var httpErr *agentclient.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusRequestEntityTooLarge {
		WriteError(w, r, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "上传文件超过大小限制", nil)
		return true
	}
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusRequestTimeout {
		WriteError(w, r, http.StatusRequestTimeout, "UPLOAD_TIMEOUT", "上传处理超时", nil)
		return true
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(r.Context().Err(), context.DeadlineExceeded) || isTimeoutError(err) {
		WriteError(w, r, http.StatusRequestTimeout, "UPLOAD_TIMEOUT", "上传处理超时", nil)
		return true
	}
	return false
}

func isTimeoutError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
