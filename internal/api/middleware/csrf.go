// csrf 中间件 — 跨站请求伪造防护
//
// 对写操作（POST/PUT/DELETE）验证 X-CSRF-Token 请求头，
// 确保请求来自拥有 CSRF Token 的合法前端页面，而非第三方网站。
//
// CSRF 防护原理：
//   - 浏览器自动携带 Cookie，但不自动携带自定义 Header
//   - 攻击者可以从第三方网站发起携带 Cookie 的请求
//   - 但无法获取到 CSRF Token（受同源策略限制）
//   - 因此写请求缺少 X-CSRF-Token 就会被拒绝
//
// 使用方式：在 chi.Router 上注册，放在 Authenticate 之后
//
//	r.Use(middleware.Authenticate(svc))
//	r.Use(middleware.CSRFProtect())
package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/luoye663/nxpanel/internal/auth"
)

// CSRFProtect 创建 CSRF 校验中间件
//
// 工作方式：
//  1. GET/HEAD/OPTIONS 请求直接放行（读操作不需要 CSRF）
//  2. POST/PUT/DELETE 请求检查 X-CSRF-Token 请求头
//  3. 从 context 中获取当前会话的 CSRF Token 哈希
//  4. 对传入 Token 做哈希后与存储的哈希比对
//
// 注意：此中间件必须在 Authenticate 之后使用
func CSRFProtect() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 读操作不需要 CSRF 校验
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			// 登录和初始化仍必须命中隐藏 gate，这里只按后缀跳过 CSRF 校验。
			if strings.HasSuffix(r.URL.Path, "/auth/login") ||
				strings.HasSuffix(r.URL.Path, "/auth/login/2fa") ||
				strings.HasSuffix(r.URL.Path, "/auth/login/recover") ||
				strings.HasSuffix(r.URL.Path, "/setup/admin") {
				next.ServeHTTP(w, r)
				return
			}

			// 获取当前会话信息（由 Authenticate 中间件注入）
			session := GetSession(r.Context())
			if session == nil {
				// 未登录，不需要 CSRF（auth 中间件会处理）
				next.ServeHTTP(w, r)
				return
			}

			// 读取 X-CSRF-Token 请求头
			token := r.Header.Get("X-CSRF-Token")
			if token == "" {
				writeCSRFError(w, r)
				return
			}

			// 验证 CSRF Token（对明文 token 做哈希后与存储的哈希比对）
			if !auth.ValidateCSRFToken(token, session.CSRFTokenHash) {
				writeCSRFError(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeCSRFError 写入 CSRF 校验失败的统一错误响应
func writeCSRFError(w http.ResponseWriter, r *http.Request) {
	rid := GetRequestID(r.Context())
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Request-ID", rid)
	w.WriteHeader(http.StatusForbidden)

	resp := map[string]any{
		"request_id": rid,
		"success":    false,
		"data":       nil,
		"error": map[string]any{
			"code":    "FORBIDDEN",
			"message": "CSRF Token 校验失败",
		},
	}
	_ = json.NewEncoder(w).Encode(resp)
}
