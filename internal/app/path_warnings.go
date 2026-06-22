package app

import (
	"fmt"
	"path/filepath"
	"strings"
)

// BuildAllowedPathRoots returns the effective agent path whitelist roots derived from config.
func BuildAllowedPathRoots(cfg *Config) []string {
	roots := make([]string, 0, 16)

	roots = appendAllowedRoot(roots, cfg.Nginx.PanelDir)
	roots = appendAllowedRoot(roots, cfg.DataDir)
	if cfg.Nginx.ConfPath != "" {
		roots = appendAllowedRoot(roots, filepath.Dir(cfg.Nginx.ConfPath))
	}
	roots = appendAllowedRoot(roots, cfg.Nginx.LogDir)
	roots = appendAllowedRoots(roots, cfg.Nginx.AllowedRootPrefixes)
	roots = appendAllowedRoots(roots, cfg.Nginx.AllowedLogPrefixes)
	roots = appendAllowedRoot(roots, "/www/wwwroot")
	roots = appendAllowedRoot(roots, "/var/www")
	roots = appendAllowedRoots(roots, cfg.Agent.AllowedRoots)

	return roots
}

func appendAllowedRoots(roots []string, values []string) []string {
	for _, value := range values {
		roots = appendAllowedRoot(roots, value)
	}
	return roots
}

func appendAllowedRoot(roots []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return roots
	}
	cleaned := filepath.Clean(value)
	for _, root := range roots {
		if root == cleaned {
			return roots
		}
	}
	return append(roots, cleaned)
}

// IsPathDeniedError matches the common agent/path-whitelist failure strings.
func IsPathDeniedError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "路径不在白名单") ||
		strings.Contains(message, "不在白名单内") ||
		strings.Contains(message, "路径不在允许的目录范围内")
}

// NewPathDeniedError builds a user-facing AppError with concrete whitelist guidance.
func NewPathDeniedError(action, pathKind, pathValue string) *AppError {
	message := fmt.Sprintf("%s失败：%s路径不在 Agent 白名单内，请在配置文件中补充白名单后重启 nxpanel-agent", action, pathKind)
	return NewAppError(ErrAgentDenied, message, map[string]any{
		"path":           pathValue,
		"path_kind":      pathKind,
		"allowed_config": allowedConfigKeysForPathKind(pathKind),
	})
}

func allowedConfigKeysForPathKind(pathKind string) []string {
	switch pathKind {
	case "根目录":
		return []string{"nginx.allowed_root_prefixes", "agent.allowed_roots"}
	case "日志":
		return []string{"nginx.allowed_log_prefixes", "agent.allowed_roots"}
	case "配置文件":
		return []string{"agent.allowed_roots"}
	default:
		return []string{"agent.allowed_roots"}
	}
}
