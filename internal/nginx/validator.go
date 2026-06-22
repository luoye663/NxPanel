// nginx 包 — 端口和路径校验器
//
// 提供 Nginx 配置相关参数的业务校验：
//   - 端口范围检查
//   - 网站根目录路径合法性检查
//   - allowed roots 白名单检查
//
// 同时约束路径前缀，避免越出允许的网站目录和日志目录。
package nginx

import (
	"fmt"
	"path/filepath"
	"strings"
)

// 允许的网站根目录前缀（可通过 SetAllowedRootPrefixes 覆盖）
var allowedRootPrefixes = []string{
	"/www/wwwroot",
	"/var/www",
}

// 允许的日志目录前缀（可通过 SetAllowedLogPrefixes 覆盖）
var allowedLogPrefixes = []string{
	"/www/wwwlogs",
	"/var/log/nginx/nxpanel",
}

// SetAllowedRootPrefixes 设置允许的网站根目录前缀白名单
func SetAllowedRootPrefixes(prefixes []string) {
	if len(prefixes) > 0 {
		allowedRootPrefixes = prefixes
	}
}

// SetAllowedLogPrefixes 设置允许的日志文件路径前缀白名单
func SetAllowedLogPrefixes(prefixes []string) {
	if len(prefixes) > 0 {
		allowedLogPrefixes = prefixes
	}
}

// ValidatePort 校验端口号是否合法
// 端口范围：1 ~ 65535
func ValidatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("端口号 %d 不合法，必须在 1-65535 之间", port)
	}
	return nil
}

// ValidateRootPath 校验网站根目录是否合法
// 必须是绝对路径，且在允许的前缀内
func ValidateRootPath(path string) error {
	if path == "" {
		return fmt.Errorf("网站根目录不能为空")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("网站根目录必须是绝对路径")
	}
	clean := filepath.Clean(path)
	for _, prefix := range allowedRootPrefixes {
		if pathWithinPrefix(clean, prefix) {
			return nil
		}
	}
	return fmt.Errorf("网站根目录必须在以下目录下: %s", strings.Join(allowedRootPrefixes, ", "))
}

// ValidateLogPath 校验日志文件路径是否合法
func ValidateLogPath(path string) error {
	if path == "" {
		return fmt.Errorf("日志路径不能为空")
	}
	if !filepath.IsAbs(path) {
		return fmt.Errorf("日志路径必须是绝对路径")
	}
	clean := filepath.Clean(path)
	for _, prefix := range allowedLogPrefixes {
		if pathWithinPrefix(clean, prefix) {
			return nil
		}
	}
	return fmt.Errorf("日志路径必须在以下目录下: %s", strings.Join(allowedLogPrefixes, ", "))
}

func pathWithinPrefix(path, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	cleanPrefix := filepath.Clean(prefix)
	return path == cleanPrefix || strings.HasPrefix(path, cleanPrefix+string(filepath.Separator))
}

// ValidateIndexFiles 校验默认首页文件参数
func ValidateIndexFiles(indexFiles string) error {
	if strings.TrimSpace(indexFiles) == "" {
		return fmt.Errorf("默认首页文件不能为空")
	}
	if strings.ContainsAny(indexFiles, ";{}\\\"\n\r") {
		return fmt.Errorf("默认首页文件包含非法字符")
	}
	return nil
}
