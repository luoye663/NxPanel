// security 包 — 域名校验
//
// 提供域名合法性校验，包括：
//   - 基本格式校验（字母、数字、连字符、点号）
//   - 标签长度限制（1-63 字符）
//   - 总长度限制（最大 253 字符）
//   - 支持通配符域名（*.example.com）
package security

import (
	"fmt"
	"strings"
)

// ValidateDomain 校验单个域名的合法性
//
// 规则：
//   - 允许小写字母、数字、连字符
//   - 标签之间用点号分隔
//   - 每个标签 1-63 个字符
//   - 总长度最大 253 个字符
//   - 标签不能以连字符开头或结尾
//   - 支持通配符前缀 *.example.com
func ValidateDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("域名不能为空")
	}

	// 转小写检查
	domain = strings.ToLower(domain)

	// 总长度检查
	if len(domain) > 253 {
		return fmt.Errorf("域名总长度不能超过 253 个字符")
	}

	// 处理通配符前缀
	d := domain
	if strings.HasPrefix(d, "*.") {
		d = d[2:] // 去掉 *. 前缀，检查后面的部分
		if d == "" {
			return fmt.Errorf("通配符域名格式不正确")
		}
	}

	// 检查不能以点号开头或结尾
	if strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
		return fmt.Errorf("域名不能以点号开头或结尾")
	}

	// 逐个标签检查
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return fmt.Errorf("域名至少包含两个标签（如 example.com）")
	}

	for _, label := range labels {
		if err := validateLabel(label); err != nil {
			return err
		}
	}

	return nil
}

// validateLabel 校验单个域名标签
func validateLabel(label string) error {
	if len(label) == 0 {
		return fmt.Errorf("域名标签不能为空")
	}
	if len(label) > 63 {
		return fmt.Errorf("域名标签长度不能超过 63 个字符: %s", label)
	}
	// 标签不能以连字符开头或结尾
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return fmt.Errorf("域名标签不能以连字符开头或结尾: %s", label)
	}
	// 只允许小写字母、数字、连字符
	for _, ch := range label {
		if !((ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-') {
			return fmt.Errorf("域名包含非法字符 '%c': %s", ch, label)
		}
	}
	return nil
}

// ValidateDomainList 批量校验域名列表，并去重
// 返回去重后的域名列表和错误
func ValidateDomainList(domains []string) ([]string, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("域名列表不能为空")
	}

	seen := make(map[string]bool)
	var result []string
	for _, d := range domains {
		d = strings.TrimSpace(strings.ToLower(d))
		if err := ValidateDomain(d); err != nil {
			return nil, fmt.Errorf("域名 %s 不合法: %w", d, err)
		}
		if seen[d] {
			continue
		}
		seen[d] = true
		result = append(result, d)
	}
	return result, nil
}
