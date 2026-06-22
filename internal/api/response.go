// api 包的统一响应结构与响应写入工具
//
//   - 成功响应：{"request_id":"xxx","success":true,"data":{...},"error":null}
//   - 失败响应：{"request_id":"xxx","success":false,"data":null,"error":{"code":"...","message":"..."}}
//
// request_id 由 middleware.RequestID 中间件注入 context，
// 通过 middleware.GetRequestID() 提取。
package api

import (
	"encoding/json"
	"net/http"

	"github.com/luoye663/nxpanel/internal/api/middleware"
)

// ErrorBody 错误响应体
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// Response 统一响应结构
// 所有 API 响应都使用此结构，前端根据 success 字段判断成功/失败
type Response struct {
	RequestID string     `json:"request_id"`
	Success   bool       `json:"success"`
	Data      any        `json:"data"`
	Error     *ErrorBody `json:"error"`
}

// WriteOK 写入成功响应（HTTP 200）
func WriteOK(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, http.StatusOK, Response{
		RequestID: middleware.GetRequestID(r.Context()),
		Success:   true,
		Data:      data,
		Error:     nil,
	})
}

// WriteCreated 写入创建成功响应（HTTP 201）
func WriteCreated(w http.ResponseWriter, r *http.Request, data any) {
	writeJSON(w, http.StatusCreated, Response{
		RequestID: middleware.GetRequestID(r.Context()),
		Success:   true,
		Data:      data,
		Error:     nil,
	})
}

// WriteError 写入错误响应
// status: HTTP 状态码
// code: 错误码（与 contracts.md 3.4 节错误码表对齐）
// msg: 人类可读的错误信息
// details: 可选的附加详情（如 nginx -t 的 stderr）
func WriteError(w http.ResponseWriter, r *http.Request, status int, code, msg string, details map[string]any) {
	writeJSON(w, status, Response{
		RequestID: middleware.GetRequestID(r.Context()),
		Success:   false,
		Data:      nil,
		Error: &ErrorBody{
			Code:    code,
			Message: msg,
			Details: details,
		},
	})
}

// writeJSON 写入 JSON 响应（内部使用）
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
