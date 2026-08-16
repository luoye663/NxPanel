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
	"net"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	proxyHostnameRE       = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	directProxyHostnameRE = regexp.MustCompile(`^[A-Za-z0-9_](?:[A-Za-z0-9._-]{0,251}[A-Za-z0-9_])?$`)
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

	// Reject characters that can terminate or reshape an Nginx directive.
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
	if strings.ContainsAny(rawURL, `"'`) {
		return errors.New("upstream URL 不允许包含引号")
	}
	if strings.ContainsAny(rawURL, "#\\ ") || strings.Contains(rawURL, "\t") {
		return errors.New("upstream URL 不允许包含注释符、反斜杠或空白字符")
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
	if parsed.User != nil {
		return errors.New("upstream URL 不允许包含用户信息")
	}

	// 分离 host 和 port
	host := parsed.Hostname()
	if host == "" {
		return errors.New("upstream URL 主机地址不能为空")
	}
	if err := validateDirectProxyHostnameOrIP(host); err != nil {
		return fmt.Errorf("upstream URL 主机地址不合法: %w", err)
	}

	// 端口检查（如果有）
	if parsed.Port() != "" {
		port := parsed.Port()
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return fmt.Errorf("upstream URL 端口必须在 1-65535 之间: %s", port)
		}
	}

	return nil
}

// ValidateHostHeader 校验反向代理的 Host header 值
//
// 允许：
//   - $host（Nginx 变量）
//   - $http_host
//   - $proxy_host
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
	if strings.ContainsAny(header, `"' 	`) {
		return errors.New("Host header 不允许包含引号或空白字符")
	}
	if header == "$host" || header == "$http_host" || header == "$proxy_host" {
		return nil
	}
	if strings.Contains(header, "$") {
		return errors.New("Host header 只允许 $host、$http_host、$proxy_host 或具体主机名/IP")
	}
	host := header
	if parsedHost, portText, err := net.SplitHostPort(header); err == nil {
		host = parsedHost
		port, parseErr := strconv.Atoi(portText)
		if parseErr != nil || port < 1 || port > 65535 {
			return errors.New("Host header 端口必须在 1-65535 之间")
		}
	}
	if err := validateProxyHostnameOrIP(host); err != nil {
		return fmt.Errorf("Host header 不合法: %w", err)
	}
	return nil
}

// ValidateProxySSLServerName accepts a literal hostname or IP only. Nginx variables
// and host:port values are intentionally rejected for deterministic TLS identity.
func ValidateProxySSLServerName(name string) error {
	if name == "" {
		return errors.New("proxy_ssl_server_name 不能为空")
	}
	if strings.Contains(name, ":") && net.ParseIP(name) == nil {
		return errors.New("proxy_ssl_server_name 不允许包含端口")
	}
	if err := validateProxyHostnameOrIP(name); err != nil {
		return fmt.Errorf("proxy_ssl_server_name 不合法: %w", err)
	}
	return nil
}

func ValidateProxySSLTrustedCertificate(path string) error {
	if path == "" {
		return errors.New("开启上游证书验证时必须提供 proxy_ssl_trusted_certificate")
	}
	if !filepath.IsAbs(path) {
		return errors.New("proxy_ssl_trusted_certificate 必须是绝对路径")
	}
	if strings.ContainsAny(path, "\x00\n\r\t ;{}\"'#\\") {
		return errors.New("proxy_ssl_trusted_certificate 包含不安全字符")
	}
	return nil
}

func ValidateProxyLocationPath(path string) error {
	if path == "" || path[0] != '/' {
		return errors.New("代理路径必须以 / 开头")
	}
	if len(path) > 1024 || strings.ContainsAny(path, "\x00\n\r;{}\"'#\\ \t") {
		return errors.New("代理路径包含不安全字符或过长")
	}
	return nil
}

func validateProxyHostnameOrIP(host string) error {
	if host == "" || strings.ContainsAny(host, "\x00\n\r;{}\"'$ \t") {
		return errors.New("只允许纯 hostname 或 IP")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if isNumericDottedHost(host) {
		return errors.New("IPv4 地址不合法")
	}
	if !proxyHostnameRE.MatchString(host) || strings.Contains(host, "..") {
		return errors.New("只允许纯 hostname 或 IP")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("hostname 标签不合法")
		}
	}
	return nil
}

func validateDirectProxyHostnameOrIP(host string) error {
	if host == "" || strings.ContainsAny(host, "\x00\n\r;{}\"'$ \t") {
		return errors.New("只允许 hostname、Docker DNS 名称或 IP")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if isNumericDottedHost(host) || !directProxyHostnameRE.MatchString(host) || strings.Contains(host, "..") {
		return errors.New("hostname 或 IP 不合法")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("hostname 标签不合法")
		}
	}
	return nil
}

func isNumericDottedHost(host string) bool {
	if !strings.Contains(host, ".") {
		return false
	}
	for i := 0; i < len(host); i++ {
		if (host[i] < '0' || host[i] > '9') && host[i] != '.' {
			return false
		}
	}
	return true
}
