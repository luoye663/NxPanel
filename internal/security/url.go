// security 包 — upstream URL 安全校验
//
// 反向代理的 upstream_url 必须满足：
//   - 只允许 http/https 协议
//   - 不允许换行、分号、花括号注入（防止 Nginx 配置注入）
//   - 不允许空字节
//   - 必须包含合法的 host 和 port
package security

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ValidateUpstreamURL 校验反向代理的 upstream URL
//
// 规则：
//  1. 不允许为空
//  2. scheme 必须是 http 或 https
//  3. 不允许包含空字节、换行、分号、花括号
//  4. 必须包含 host
//  5. host 不允许包含路径穿越或特殊字符
//  6. 端口（如果有）必须是合法数字
func ValidateUpstreamURL(rawURL string) error {
	if rawURL == "" {
		return errors.New("upstream URL 不能为空")
	}

	// 注入检查：空字节、换行、分号、花括号
	if strings.Contains(rawURL, "\x00") {
		return errors.New("upstream URL 不允许包含空字节")
	}
	if strings.ContainsAny(rawURL, "\n\r") {
		return errors.New("upstream URL 不允许包含换行符")
	}
	if strings.Contains(rawURL, ";") {
		return errors.New("upstream URL 不允许包含分号")
	}
	if strings.ContainsAny(rawURL, "{}") {
		return errors.New("upstream URL 不允许包含花括号")
	}

	// 解析 URL
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("upstream URL 格式不合法: %w", err)
	}

	// scheme 检查：只允许 http/https
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("upstream URL 只允许 http 或 https 协议，当前: %s", parsed.Scheme)
	}

	// host 检查
	if parsed.Host == "" {
		return errors.New("upstream URL 必须包含主机地址")
	}

	// 分离 host 和 port
	host := parsed.Hostname()
	if host == "" {
		return errors.New("upstream URL 主机地址不能为空")
	}

	// 端口检查（如果有）
	if parsed.Port() != "" {
		port := parsed.Port()
		for _, ch := range port {
			if ch < '0' || ch > '9' {
				return fmt.Errorf("upstream URL 端口不合法: %s", port)
			}
		}
	}

	return nil
}

// ValidateHostHeader 校验反向代理的 Host header 值
//
// 允许：
//   - $host（Nginx 变量）
//   - $http_host
//   - 具体域名（如 backend.example.com）
//
// 不允许空值、换行、分号、花括号
func ValidateHostHeader(header string) error {
	if header == "" {
		return errors.New("Host header 不能为空")
	}
	if strings.Contains(header, "\x00") {
		return errors.New("Host header 不允许包含空字节")
	}
	if strings.ContainsAny(header, "\n\r") {
		return errors.New("Host header 不允许包含换行符")
	}
	if strings.Contains(header, ";") {
		return errors.New("Host header 不允许包含分号")
	}
	if strings.ContainsAny(header, "{}") {
		return errors.New("Host header 不允许包含花括号")
	}
	return nil
}
