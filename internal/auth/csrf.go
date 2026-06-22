// auth 包 — CSRF Token 管理
//
// CSRF（跨站请求伪造）防护原理：
//   1. 登录成功时，服务端生成随机 CSRF Token
//   2. Token 的 SHA-256 哈希存入 sessions 表
//   3. Token 明文通过响应体返回给前端（前端存入 localStorage 或 meta 标签）
//   4. 前端在写请求（POST/PUT/DELETE）时通过 X-CSRF-Token 请求头携带 Token
//   5. 服务端中间件对传入 Token 做哈希后与数据库中的哈希比对
//
// 这种设计确保：
//   - 数据库不存储 CSRF Token 明文（即使数据库泄露也无法伪造请求）
//   - Cookie（会话）+ Header（CSRF）双重验证，防止 CSRF 攻击
package auth

import (
	"crypto/subtle"
)

// ValidateCSRFToken 验证客户端提交的 CSRF Token 是否与数据库中存储的哈希匹配
//
// 参数：
//   - token: 客户端通过 X-CSRF-Token 请求头传入的明文 Token
//   - storedHash: 数据库 sessions 表中存储的 SHA-256 哈希
//
// 返回：
//   - true 表示 Token 有效
func ValidateCSRFToken(token, storedHash string) bool {
	if token == "" || storedHash == "" {
		return false
	}
	computed := sha256Hash(token)
	return subtle.ConstantTimeCompare([]byte(computed), []byte(storedHash)) == 1
}
