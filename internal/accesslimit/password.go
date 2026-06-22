package accesslimit

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

func containsNginxInjectionChars(s string) bool {
	return strings.ContainsAny(s, ";{}\\\"\n\r")
}

func GenerateHtpasswdEntry(username, password string) string {
	h := sha1.Sum([]byte(password))
	hash := "{SHA}" + base64.StdEncoding.EncodeToString(h[:])
	return username + ":" + hash
}

func GenerateHtpasswdContent(username, password string) string {
	return GenerateHtpasswdEntry(username, password) + "\n"
}

func NewRuleID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return "ar_" + hex.EncodeToString(b)
}

func NewDenyRuleID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return "dr_" + hex.EncodeToString(b)
}

func SanitizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func ValidatePath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("路径不能为空")
	}
	if containsNginxInjectionChars(p) {
		return fmt.Errorf("路径包含非法字符")
	}
	return nil
}

func SanitizePattern(p string) string {
	return strings.TrimSpace(p)
}

func ValidateExtensionPattern(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("扩展名模式不能为空")
	}
	if containsNginxInjectionChars(p) {
		return fmt.Errorf("扩展名模式包含非法字符")
	}
	return nil
}

func NormalizeIPLimitEntries(entries []string) ([]string, error) {
	seen := make(map[string]struct{}, len(entries))
	var result []string
	for _, raw := range entries {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		normalized, err := normalizeIPLimitEntry(item)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("请填写至少一个可访问 IP 或 CIDR")
	}
	return result, nil
}

func normalizeIPLimitEntry(value string) (string, error) {
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.String(), nil
	}
	if ip, err := netip.ParseAddr(value); err == nil {
		return ip.String(), nil
	}
	if ip, _, err := net.ParseCIDR(value); err == nil && ip != nil {
		return value, nil
	}
	return "", fmt.Errorf("无效的 IP 或 CIDR: %s", value)
}

func RenderIPAllowRule(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	var buf strings.Builder
	buf.WriteString("# === IP 白名单 ===\n")
	for _, entry := range entries {
		buf.WriteString("allow ")
		buf.WriteString(entry)
		buf.WriteString(";\n")
	}
	buf.WriteString("deny all;\n")
	return buf.String()
}

func RenderIPBlacklistRule(entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	var buf strings.Builder
	buf.WriteString("# === IP 黑名单 ===\n")
	for _, entry := range entries {
		buf.WriteString("deny ")
		buf.WriteString(entry)
		buf.WriteString(";\n")
	}
	return buf.String()
}

func RenderAuthRule(ruleID, path, htpasswdPath string) string {
	return fmt.Sprintf(
		"location %s {\n    auth_basic \"Restricted\";\n    auth_basic_user_file %s;\n}\n",
		path, htpasswdPath,
	)
}

func RenderDenyExtensionRule(pattern string) string {
	extensions := strings.Split(pattern, ",")
	var cleaned []string
	for _, ext := range extensions {
		ext = strings.TrimSpace(ext)
		ext = strings.TrimPrefix(ext, ".")
		if ext == "" {
			continue
		}
		cleaned = append(cleaned, ext)
	}
	if len(cleaned) == 0 {
		return ""
	}
	return fmt.Sprintf("location ~* \\.(%s)$ {\n    deny all;\n}\n",
		strings.Join(cleaned, "|"))
}

func RenderDenyPathRule(pattern string) string {
	path := SanitizePath(pattern)
	return fmt.Sprintf("location %s {\n    deny all;\n}\n", path)
}
