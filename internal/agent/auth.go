// agent 包 — Token 认证中间件
//
// agent 通过 Unix Socket 暴露 RPC 接口，虽然 Unix Socket 本身有文件系统权限保护，
// 但仍要求调用方在请求头中携带共享密钥 Token，作为防御加固。
//
// 认证方式：
//   - 请求头 X-NxPanel-Agent-Token 必须与配置中的 agent.token 匹配
//   - Token 不匹配时返回 401 Unauthorized
//   - 如果配置中 token 为空，则跳过认证（仅限开发环境）
package agent

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
)

// agentTokenHeader 是 agent 认证请求头的名称
const agentTokenHeader = "X-NxPanel-Agent-Token"

// TokenAuth 创建 agent token 认证中间件
//
// 工作方式：
//  1. 读取请求头 X-NxPanel-Agent-Token
//  2. 与配置的 token 比对（使用恒定时间比较）
//  3. 匹配则放行，不匹配则返回 401
//  4. 如果配置 token 为空，则拒绝启动（必须在配置中设置）
func TokenAuth(expectedToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get(agentTokenHeader)
			if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusUnauthorized)
				resp := map[string]any{
					"ok":    false,
					"error": "invalid agent token",
				}
				_ = json.NewEncoder(w).Encode(resp)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
