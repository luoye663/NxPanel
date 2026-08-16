package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

func UploadReadDeadline(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			controller := http.NewResponseController(w)
			if err := controller.SetReadDeadline(time.Now().Add(timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				writeUploadDeadlineError(w, GetRequestID(r.Context()), http.StatusInternalServerError, "无法设置上传读取超时")
				return
			}
			if err := controller.SetWriteDeadline(time.Now().Add(timeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				writeUploadDeadlineError(w, GetRequestID(r.Context()), http.StatusInternalServerError, "无法设置上传响应超时")
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
			defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func writeUploadDeadlineError(w http.ResponseWriter, requestID string, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"request_id": requestID,
		"success":    false,
		"data":       nil,
		"error":      map[string]any{"code": "UPLOAD_TIMEOUT", "message": message},
	})
}
