// recover 中间件 — 捕获 handler 中的 panic，返回统一的 JSON 错误响应
//
// 为什么不使用 chi 自带的 middleware.Recoverer？
// chi 的 Recoverer 默认返回 500 纯文本响应，不符合我们的 API 统一响应契约。
// 我们需要在 panic 时也返回标准的 JSON 格式：
//
//	{"request_id":"xxx","success":false,"error":{"code":"INTERNAL_ERROR","message":"..."}}
package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recoverer 返回一个 panic 恢复中间件
//
// 工作方式：
//  1. 使用 defer + recover 捕获 handler 中的 panic
//  2. 记录 panic 堆栈到 slog（包含 request_id）
//  3. 返回 HTTP 500 + 统一 JSON 错误响应
func Recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// 从 context 获取 request_id（由 RequestID 中间件注入）
				rid := GetRequestID(r.Context())

				// 记录 panic 详情到日志（包含堆栈信息）
				slog.Error("handler panic 已恢复",
					"request_id", rid,
					"error", err,
					"stack", string(debug.Stack()),
					"path", r.URL.Path,
					"method", r.Method,
				)

				// 返回统一的 JSON 错误响应
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("X-Request-ID", rid)
				w.WriteHeader(http.StatusInternalServerError)

				resp := map[string]any{
					"request_id": rid,
					"success":    false,
					"data":       nil,
					"error": map[string]any{
						"code":    "INTERNAL_ERROR",
						"message": "服务器内部错误，请稍后重试",
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			}
		}()

		next.ServeHTTP(w, r)
	})
}
