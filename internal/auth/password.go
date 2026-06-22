// auth 包 — 密码哈希与验证
//
// 使用 bcrypt 算法对管理员密码进行哈希存储。
// bcrypt 自带盐值（salt），且可以调节 cost 参数控制计算强度，
// 是目前最推荐的密码哈希算法之一。
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost 是 bcrypt 的计算成本因子
// 值越高，哈希计算越慢（越安全），但也会消耗更多 CPU
// 推荐值：10-12，12 在现代服务器上约需 250ms
const bcryptCost = 12

// PasswordAlgo 表示当前使用的密码哈希算法标识
// 存储到 admin_account.password_algo 字段，方便未来迁移算法
const PasswordAlgo = "bcrypt"

// HashPassword 对明文密码进行 bcrypt 哈希
//
// 参数：
//   - password: 明文密码
//
// 返回：
//   - 哈希后的密码字符串（包含算法、cost、盐和哈希值）
//   - 错误信息
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt 哈希失败: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword 验证明文密码是否与存储的哈希匹配
//
// 参数：
//   - password: 用户输入的明文密码
//   - hash: 数据库中存储的 bcrypt 哈希字符串
//
// 返回：
//   - true 表示密码正确，false 表示密码错误
func VerifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
