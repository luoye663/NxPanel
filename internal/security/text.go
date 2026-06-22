// security 包 — 文本安全检查
//
// 提供输入文本的安全验证，防止注入攻击：
//   - 空字节检查：防止 C 字符串截断攻击
//   - 换行检查：防止日志注入和头部注入
//   - 大小限制：防止拒绝服务攻击
package security

import (
	"errors"
	"strings"
)

// ValidateText 验证文本内容的安全性
//
// 检查项：
//   - 不允许包含空字节 \x00
//   - 不允许包含换行 \n \r（防止头部注入）
//   - 长度不超过 maxLen
func ValidateText(text string, maxLen int) error {
	if len(text) > maxLen {
		return errors.New("文本长度超过限制")
	}
	if strings.Contains(text, "\x00") {
		return errors.New("文本不允许包含空字节")
	}
	if strings.ContainsAny(text, "\n\r") {
		return errors.New("文本不允许包含换行符")
	}
	return nil
}

// ValidateContent 验证文件内容的安全性
//
// 与 ValidateText 不同，文件内容允许换行但检查空字节和大小限制。
// 用于验证 Nginx 配置文件内容等。
func ValidateContent(content string, maxLen int) error {
	if len(content) > maxLen {
		return errors.New("内容大小超过限制")
	}
	if strings.Contains(content, "\x00") {
		return errors.New("内容不允许包含空字节")
	}
	return nil
}
