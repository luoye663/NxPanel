// auth 包测试 — 密码哈希、Session 管理、CSRF 验证
package auth

import (
	"testing"
)

// TestHashAndVerifyPassword 测试密码哈希与验证
func TestHashAndVerifyPassword(t *testing.T) {
	password := "test-password-123"

	// 哈希密码
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword 失败: %v", err)
	}

	// 验证哈希不为空且不是明文
	if hash == "" {
		t.Fatal("哈希不应为空")
	}
	if hash == password {
		t.Fatal("哈希不应等于明文密码")
	}

	// 验证正确密码
	if !VerifyPassword(password, hash) {
		t.Error("正确密码应验证通过")
	}

	// 验证错误密码
	if VerifyPassword("wrong-password", hash) {
		t.Error("错误密码应验证失败")
	}

	// 验证空密码
	if VerifyPassword("", hash) {
		t.Error("空密码应验证失败")
	}
}

// TestHashPasswordDifferentEachTime 测试同一密码每次哈希结果不同（因为有随机盐）
func TestHashPasswordDifferentEachTime(t *testing.T) {
	password := "same-password"
	hash1, _ := HashPassword(password)
	hash2, _ := HashPassword(password)

	if hash1 == hash2 {
		t.Error("同一密码的两次哈希应不同（bcrypt 使用随机盐）")
	}
}

// TestPasswordAlgo 测试密码算法标识
func TestPasswordAlgo(t *testing.T) {
	if PasswordAlgo != "bcrypt" {
		t.Errorf("PasswordAlgo 期望 bcrypt，实际 %s", PasswordAlgo)
	}
}
