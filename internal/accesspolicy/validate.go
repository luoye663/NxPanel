package accesspolicy

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"unicode/utf8"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
var extension = regexp.MustCompile(`^\.?[A-Za-z0-9_-]{1,64}$`)
var refererDomain = regexp.MustCompile(`^(?:\*\.)?[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)

func Validate(p Policy) error {
	if p.Mode != "" && p.Mode != "legacy" && p.Mode != "unified" {
		return fmt.Errorf("无效访问策略模式")
	}
	if len(p.Rules) > MaxRules {
		return fmt.Errorf("访问规则不能超过 %d 条", MaxRules)
	}
	if err := validateAction(p.DefaultAction); err != nil {
		return fmt.Errorf("默认动作: %w", err)
	}
	seen := map[string]bool{}
	entries := 0
	for _, r := range p.Rules {
		if !identifier.MatchString(r.ID) || r.ID == "default" || seen[r.ID] {
			return fmt.Errorf("规则 ID 无效或重复: %s", r.ID)
		}
		seen[r.ID] = true
		if len(r.Name) > 256 || !utf8.ValidString(r.Name) {
			return fmt.Errorf("规则名称过长或无效")
		}
		if r.Match != "all" && r.Match != "any" {
			return fmt.Errorf("规则 %s 组合方式必须为 all 或 any", r.ID)
		}
		if len(r.Conditions) > MaxConditions {
			return fmt.Errorf("规则条件不能超过 %d 个", MaxConditions)
		}
		if err := validateAction(r.Action); err != nil {
			return fmt.Errorf("规则 %s: %w", r.ID, err)
		}
		for _, c := range r.Conditions {
			entries += len(c.Values)
			if entries > MaxConditionValues {
				return fmt.Errorf("条件值总数不能超过 %d", MaxConditionValues)
			}
			if err := validateCondition(c); err != nil {
				return fmt.Errorf("规则 %s: %w", r.ID, err)
			}
		}
	}
	return nil
}
func validateAction(a Action) error {
	switch a.Type {
	case "allow":
	case "deny":
		if a.StatusCode != 0 && (a.StatusCode < 400 || a.StatusCode > 599) {
			return fmt.Errorf("拒绝响应状态码必须在 400–599 之间")
		}
		if a.ResponseType != "" && a.ResponseType != "text" && a.ResponseType != "html" {
			return fmt.Errorf("响应类型必须为 text 或 html")
		}
		if len(a.ResponseBody) > 65536 || !utf8.ValidString(a.ResponseBody) || strings.ContainsRune(a.ResponseBody, 0) {
			return fmt.Errorf("响应正文无效或超过 64 KiB")
		}
	case "auth":
		if len(a.AccountIDs) == 0 || len(a.AccountIDs) > 1024 {
			return fmt.Errorf("密码验证需要 1–1024 个账户")
		}
		for _, id := range a.AccountIDs {
			if !identifier.MatchString(id) {
				return fmt.Errorf("无效账户 ID")
			}
		}
	default:
		return fmt.Errorf("无效访问动作: %s", a.Type)
	}
	return nil
}
func validateCondition(c Condition) error {
	if len(c.Values) == 0 {
		return fmt.Errorf("条件至少需要一个值")
	}
	if c.Kind != "path" && c.Operator != "" && c.Operator != "in" {
		return fmt.Errorf("无效条件操作符")
	}
	for _, v := range c.Values {
		if len(v) > 4096 || !utf8.ValidString(v) || strings.ContainsAny(v, "\x00\r\n") {
			return fmt.Errorf("条件值无效或过长")
		}
		switch c.Kind {
		case "ip":
			if _, err := parseNetwork(v); err != nil {
				return fmt.Errorf("无效 IP/CIDR: %s", v)
			}
		case "country":
			v = strings.ToUpper(v)
			if len(v) != 2 || v[0] < 'A' || v[0] > 'Z' || v[1] < 'A' || v[1] > 'Z' {
				return fmt.Errorf("无效国家代码: %s", v)
			}
		case "path":
			if (c.Operator != "exact" && c.Operator != "prefix") || !strings.HasPrefix(v, "/") || strings.ContainsAny(v, "?#") {
				return fmt.Errorf("路径须以 / 开头且操作符为 exact 或 prefix")
			}
		case "extension":
			if !extension.MatchString(v) {
				return fmt.Errorf("无效文件后缀: %s", v)
			}
		case "referer":
			if _, err := refererPattern(v); err != nil {
				return err
			}
		default:
			return fmt.Errorf("无效条件类型: %s", c.Kind)
		}
	}
	return nil
}
func parseNetwork(v string) (netip.Prefix, error) {
	if a, err := netip.ParseAddr(v); err == nil {
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	p, err := netip.ParsePrefix(v)
	if err != nil {
		return netip.Prefix{}, err
	}
	return p.Masked(), nil
}

// refererPattern is shared by the preview and the native case-insensitive map.
func refererPattern(v string) (string, error) {
	if v == "server_names" {
		return "", nil
	}
	if v == "none" {
		return `\A\z`, nil
	}
	if v == "blocked" {
		return "", nil
	}
	host, suffix, _ := strings.Cut(v, "/")
	if !refererDomain.MatchString(host) {
		return "", fmt.Errorf("无效 Referer 域名: %s", v)
	}
	wildcard := strings.HasPrefix(host, "*.")
	host = strings.TrimPrefix(host, "*.")
	pattern := `\Ahttps?://`
	if wildcard {
		pattern += `(?:[^/:]+\.)?`
	}
	pattern += regexp.QuoteMeta(host) + `(?::[0-9]+)?`
	if suffix != "" {
		pattern += `/` + regexp.QuoteMeta(suffix)
	} else {
		pattern += `(?:/|\z)`
	}
	return pattern, nil
}
func effectiveStatus(a Action) int {
	if a.StatusCode == 0 {
		return 403
	}
	return a.StatusCode
}
