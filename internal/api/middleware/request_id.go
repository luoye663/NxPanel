// middleware 包提供 API 服务器使用的 HTTP 中间件
package middleware

import (
	"context"
	"net/http"

	"github.com/luoye663/nxpanel/internal/app"
)

// ============================================================
// Context Key 定义
// ============================================================

// contextKey 是中间件用于向 context 注入值的键类型
// 使用自定义类型避免与第三方包的 context key 冲突
type contextKey string

const (
	// RequestIDKey 是 request_id 在 context 中的键
	RequestIDKey contextKey = "request_id"
)

// GetRequestID 从 context 中提取 request_id
// 供 response.go 和 handler 调用
// 安全处理 nil context
func GetRequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(RequestIDKey).(string); ok {
		return v
	}
	return ""
}

// WithRequestID 向 context 中注入 request_id
// 主要用于测试中模拟 RequestID 中间件的效果
func WithRequestID(ctx context.Context, rid string) context.Context {
	return context.WithValue(ctx, RequestIDKey, rid)
}

// ============================================================
// RequestID 中间件
// ============================================================

// RequestID 为每个请求生成唯一的 request_id 并注入 context
//
// 工作方式：
//  1. 检查请求头 X-Request-ID（支持上游代理传入）
//  2. 如果没有，使用 app.NewRequestID() 生成新 ID
//  3. 将 ID 注入 request context
//  4. 设置到响应头 X-Request-ID，方便前端和调试
//
// 生成的 request_id 格式：req_{timestamp_hex}_{random_hex}
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if rid == "" || !isValidRequestID(rid) {
			rid = app.NewRequestID()
		}

		// 将 request_id 注入 context，后续 handler 和 response 可读取
		ctx := context.WithValue(r.Context(), RequestIDKey, rid)

		// 在响应头中返回 request_id，方便前端和调试追踪
		w.Header().Set("X-Request-ID", rid)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isValidRequestID(rid string) bool {
	if len(rid) > 128 {
		return false
	}
	for _, c := range rid {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
