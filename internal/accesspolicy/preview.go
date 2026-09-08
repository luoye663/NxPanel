package accesspolicy

import (
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"regexp"
	"strings"
)

func Preview(p Policy, req PreviewRequest, ctx RenderContext) (*PreviewResult, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	ip, err := netip.ParseAddr(req.IP)
	if err != nil {
		return nil, fmt.Errorf("无效预览 IP")
	}
	ip = ip.Unmap()
	uri := strings.SplitN(req.Path, "?", 2)[0]
	uri, err = url.PathUnescape(uri)
	if err != nil || !strings.HasPrefix(uri, "/") || strings.ContainsRune(uri, 0) {
		return nil, fmt.Errorf("无效预览路径")
	}
	trailing := strings.HasSuffix(uri, "/") || strings.HasSuffix(uri, "/.") || strings.HasSuffix(uri, "/..")
	depth := 0
	for _, part := range strings.Split(uri, "/") {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("预览路径越过根目录")
			}
		default:
			depth++
		}
	}
	uri = path.Clean(uri)
	if trailing && uri != "/" {
		uri += "/"
	}
	country := strings.ToUpper(req.Country)
	if country == "" {
		country = lookupCountry(ip, ctx.CountryNetworks)
	}
	result := &PreviewResult{Rules: []RulePreview{}, MatchedRuleID: "default", Action: p.DefaultAction}
	found := false
	for _, r := range p.Rules {
		item := RulePreview{ID: r.ID, Conditions: []bool{}, Evaluated: r.Enabled && !r.SourceDisabled && !found}
		if r.Enabled && !r.SourceDisabled {
			match := r.Match == "all"
			for _, c := range r.Conditions {
				c = expandServerNames(c, ctx.ServerNames)
				hit := matches(c, ip, country, uri, req.Referer)
				item.Conditions = append(item.Conditions, hit)
				if r.Match == "all" {
					match = match && hit
				} else {
					match = match || hit
				}
			}
			item.Matched = match
			if !found && match {
				result.MatchedRuleID = r.ID
				result.Action = r.Action
				found = true
			}
		}
		result.Rules = append(result.Rules, item)
	}
	result.RequiresAuthentication = result.Action.Type == "auth"
	return result, nil
}
func lookupCountry(ip netip.Addr, networks map[string][]string) string {
	country := "ZZ"
	bits := -1
	for code, list := range networks {
		for _, v := range list {
			p, err := parseNetwork(v)
			if err == nil && p.Contains(ip) && p.Bits() > bits {
				country = strings.ToUpper(code)
				bits = p.Bits()
			}
		}
	}
	return country
}
func matches(c Condition, ip netip.Addr, country, uri, referer string) bool {
	hit := false
	for _, v := range c.Values {
		switch c.Kind {
		case "ip":
			p, _ := parseNetwork(v)
			hit = p.Contains(ip)
		case "country":
			hit = strings.EqualFold(country, v)
		case "path":
			if c.Operator == "exact" {
				hit = uri == v
			} else {
				hit = strings.HasPrefix(uri, v)
			}
		case "extension":
			hit = strings.HasSuffix(strings.ToLower(uri), "."+strings.ToLower(strings.TrimPrefix(v, ".")))
		case "referer":
			if v == "blocked" {
				lower := strings.ToLower(referer)
				hit = referer != "" && !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://")
			} else {
				pattern, _ := refererPattern(v)
				hit = regexp.MustCompile("(?i)" + pattern).MatchString(referer)
			}
		}
		if hit {
			break
		}
	}
	if c.Negate {
		return !hit
	}
	return hit
}
