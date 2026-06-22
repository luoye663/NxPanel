// auth 包测试 — CSRF Token 验证
package auth

import (
	"testing"
)

// TestValidateCSRFToken 测试 CSRF Token 验证逻辑
func TestValidateCSRFToken(t *testing.T) {
	// 生成一个合法的 token 和它的哈希
	token := "abc123test-token-xyz"
	storedHash := sha256Hash(token)

	// 验证正确 token
	if !ValidateCSRFToken(token, storedHash) {
		t.Error("正确的 CSRF token 应验证通过")
	}

	// 验证错误 token
	if ValidateCSRFToken("wrong-token", storedHash) {
		t.Error("错误的 CSRF token 应验证失败")
	}

	// 空 token
	if ValidateCSRFToken("", storedHash) {
		t.Error("空 token 应验证失败")
	}

	// 空 hash
	if ValidateCSRFToken(token, "") {
		t.Error("空 hash 应验证失败")
	}

	// 两者都空
	if ValidateCSRFToken("", "") {
		t.Error("空 token 和空 hash 应验证失败")
	}
}

// TestSHA256Hash 测试 SHA-256 哈希函数
func TestSHA256Hash(t *testing.T) {
	input := "test-input"
	hash1 := sha256Hash(input)
	hash2 := sha256Hash(input)

	// 同一输入应产生相同哈希
	if hash1 != hash2 {
		t.Error("同一输入的哈希应相同")
	}

	// 不同输入应产生不同哈希
	hash3 := sha256Hash("different-input")
	if hash1 == hash3 {
		t.Error("不同输入的哈希应不同")
	}

	// 哈希长度应为 64 个字符（SHA-256 = 32 字节 = 64 十六进制字符）
	if len(hash1) != 64 {
		t.Errorf("SHA-256 哈希长度期望 64，实际 %d", len(hash1))
	}
}
