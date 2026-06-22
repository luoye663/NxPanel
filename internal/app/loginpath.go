package app

import (
	"crypto/rand"
	"errors"
	"strings"
)

const loginPathAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var reservedLoginPaths = map[string]bool{
	"/":       true,
	"/setup":  true,
	"/login":  true,
	"/api":    true,
	"/assets": true,
	"/health": true,
	"/auth":   true,
}

// GenerateLoginPath 使用 crypto/rand 生成不可预测入口，避免固定登录地址被扫描器命中。
func GenerateLoginPath() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败极罕见；返回空值让调用方按校验失败处理，而不是降级到 math/rand。
		return ""
	}
	var b strings.Builder
	b.Grow(17)
	b.WriteByte('/')
	for i := 0; i < 16; i++ {
		b.WriteByte(loginPathAlphabet[int(buf[i])%len(loginPathAlphabet)])
	}
	return strings.ToLower(b.String())
}

// ValidateLoginPath 限制为单段路径，避免与公开资源、API 前缀或旧固定入口冲突。
func ValidateLoginPath(path string) error {
	if path == "" {
		return errors.New("login_path 不能为空")
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New("login_path 必须以 / 开头")
	}
	if strings.ContainsAny(path, "\x00\n\r") {
		return errors.New("login_path 包含非法字符")
	}
	if reservedLoginPaths[path] {
		return errors.New("login_path 不能使用保留路径")
	}
	token := strings.TrimPrefix(path, "/")
	if strings.Contains(token, "/") {
		return errors.New("login_path 只能包含一个路径段")
	}
	if len(token) < 8 || len(token) > 64 {
		return errors.New("login_path token 长度必须在 8-64 之间")
	}
	for _, ch := range token {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return errors.New("login_path 只能包含字母、数字、- 或 _")
	}
	return nil
}
