package accesspolicy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

// LegacyDraft reads the old configuration without persisting or activating it.
// A sequential policy cannot reproduce Nginx's independent access phases and
// location selection exactly; warnings are part of the editable draft.
func (r *Repo) LegacyDraft(siteID string) (*Policy, error) {
	site, err := repo.NewSiteRepo(r.db).GetByID(siteID)
	if err != nil {
		return nil, err
	}
	if site == nil {
		return nil, sql.ErrNoRows
	}
	policy := &Policy{Mode: "legacy", Rules: []Rule{}, DefaultAction: Action{Type: "allow"},
		ApplyStatus: "draft", Warnings: []string{}}
	ipRules, err := repo.NewIPWhitelistRuleRepo(r.db).ListBySiteID(siteID)
	if err != nil {
		return nil, err
	}
	var allowRules []Rule
	var allowIPs []string
	for _, old := range ipRules {
		entries, err := legacyStringList(old.IPsJSON, "IP", old.Name)
		if err != nil {
			return nil, err
		}
		rule := legacyRule("ip", old.ID, old.Name, old.Enabled,
			[]Condition{{Kind: "ip", Operator: "in", Values: entries}}, Action{Type: "allow"})
		if old.RuleType == "deny" || old.RuleType == "blacklist" {
			rule.Action = Action{Type: "deny", StatusCode: 403}
			policy.Rules = append(policy.Rules, rule)
		} else {
			allowRules = append(allowRules, rule)
			if old.Enabled {
				allowIPs = append(allowIPs, entries...)
			}
		}
	}
	if len(allowIPs) > 0 {
		policy.Rules = append(policy.Rules, legacyRule("ip_fallback", "whitelist_unmatched",
			"旧 IP 白名单：未命中时拒绝", true,
			[]Condition{{Kind: "ip", Operator: "in", Values: allowIPs, Negate: true}},
			Action{Type: "deny", StatusCode: 403}))
		policy.Warnings = append(policy.Warnings,
			"旧 IP 白名单的未命中拒绝已转换为显式规则；白名单放行暂放在密码、防盗链等限制之后。放行现在会结束检查，移动规则会改变访问权限。")
	}
	geoRepo := repo.NewGeoAccessRepo(r.db)
	geoSettings, err := geoRepo.GetSiteSettings(siteID)
	if err != nil {
		return nil, err
	}
	geoRules, err := geoRepo.ListRules(siteID)
	if err != nil {
		return nil, err
	}
	var geoAllows []Rule
	var earlierCountries []string
	for _, old := range geoRules {
		countries, err := legacyStringList(old.CountriesJSON, "地域", old.Name)
		if err != nil {
			return nil, err
		}
		conditions := []Condition{{Kind: "country", Operator: "in", Values: countries}}
		if len(earlierCountries) > 0 {
			conditions = append(conditions, Condition{Kind: "country", Operator: "in", Values: append([]string(nil), earlierCountries...), Negate: true})
		}
		if len(allowIPs) > 0 {
			conditions = append(conditions, Condition{Kind: "ip", Operator: "in", Values: allowIPs, Negate: true})
		}
		action := Action{Type: "allow"}
		if old.Action != "allow" {
			action = Action{Type: "deny", StatusCode: 403}
			if old.Action == "deny_444" {
				action.StatusCode = 444
			}
		}
		rule := legacyRule("geo", old.ID, old.Name, geoSettings.Enabled && old.Enabled, conditions, action)
		if action.Type == "allow" {
			geoAllows = append(geoAllows, rule)
		} else {
			policy.Rules = append(policy.Rules, rule)
		}
		if old.Enabled {
			earlierCountries = append(earlierCountries, countries...)
		}
	}
	var geoFallback *Rule
	if len(geoRules) > 0 || geoSettings.CreatedAt != "" {
		conditions := []Condition{}
		if len(earlierCountries) > 0 {
			conditions = append(conditions, Condition{Kind: "country", Operator: "in", Values: earlierCountries, Negate: true})
		}
		if len(allowIPs) > 0 {
			conditions = append(conditions, Condition{Kind: "ip", Operator: "in", Values: allowIPs, Negate: true})
		}
		action := Action{Type: "allow"}
		if geoSettings.DefaultAction != "allow" {
			action = Action{Type: "deny", StatusCode: geoSettings.DefaultStatusCode,
				ResponseType: geoSettings.DefaultResponseType, ResponseBody: geoSettings.DefaultResponseBody}
		}
		rule := legacyRule("geo_fallback", "unmatched", "旧地域策略：未命中时的响应", geoSettings.Enabled, conditions, action)
		if action.Type == "deny" {
			policy.Rules = append(policy.Rules, rule)
		} else {
			geoFallback = &rule
		}
		policy.Warnings = append(policy.Warnings,
			"地域规则已加入显式条件，以保留旧地域首条命中及 IP 白名单例外；原地域默认响应也已列为规则。请检查这些条件后再调整顺序。")
	}
	denyRules, err := repo.NewDenyRuleRepo(r.db).ListBySiteID(siteID)
	if err != nil {
		return nil, err
	}
	for _, old := range denyRules {
		conditions := []Condition{}
		if old.ExtensionPattern != "" {
			conditions = append(conditions, Condition{Kind: "extension", Operator: "in", Values: legacyCSV(old.ExtensionPattern)})
		}
		if old.PathPattern != "" {
			conditions = append(conditions, legacyPath(old.PathPattern))
		}
		if len(conditions) == 0 && old.Pattern != "" {
			// Older schemas used pattern + deny_type before separate fields.
			if old.DenyType == "extension" {
				conditions = append(conditions, Condition{Kind: "extension", Operator: "in", Values: legacyCSV(old.Pattern)})
			} else {
				conditions = append(conditions, legacyPath(old.Pattern))
			}
		}
		if len(conditions) == 0 {
			// Never turn an unrepresentable old rule into a catch-all denial.
			conditions = []Condition{{Kind: "path", Operator: "legacy", Values: []string{old.Pattern}}}
		}
		rule := legacyRule("deny", old.ID, old.Name, old.Enabled, conditions, Action{Type: "deny", StatusCode: 403})
		// Old suffix and path emitted two independent denying locations.
		rule.Match = "any"
		policy.Rules = append(policy.Rules, rule)
	}
	hotlinkRules, err := repo.NewHotlinkRuleRepo(r.db).ListBySiteID(siteID)
	if err != nil {
		return nil, err
	}
	for _, old := range hotlinkRules {
		var referers []string
		for _, value := range legacyCSV(old.Referers) {
			if value == "server_names" {
				domains, err := legacyStringList(site.DomainsJSON, "站点域名", site.PrimaryDomain)
				if err != nil {
					return nil, err
				}
				referers = append(referers, domains...)
			} else {
				referers = append(referers, value)
			}
		}
		if old.AllowEmptyReferer {
			referers = append(referers, "none")
		}
		policy.Rules = append(policy.Rules, legacyRule("hotlink", old.ID, old.Name, old.Enabled,
			[]Condition{{Kind: "extension", Operator: "in", Values: legacyCSV(old.Extensions)},
				{Kind: "referer", Operator: "in", Values: referers, Negate: true}},
			Action{Type: "deny", StatusCode: old.BlockStatus}))
	}
	authRepo := repo.NewAuthRuleRepo(r.db)
	authRules, err := authRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, err
	}
	for _, old := range authRules {
		ids, err := authRepo.GetAccountIDs(old.ID)
		if err != nil {
			return nil, err
		}
		policy.Rules = append(policy.Rules, legacyRule("auth", old.ID, old.Name, old.Enabled,
			[]Condition{legacyPath(old.Path)}, Action{Type: "auth", AccountIDs: ids}))
	}
	proxyRepo := repo.NewProxyRepo(r.db)
	proxies, err := proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, err
	}
	proxyAuthCount := 0
	for _, old := range proxies {
		if !old.AuthEnabled {
			continue
		}
		ids, err := proxyRepo.GetAccountIDs(old.ID)
		if err != nil {
			return nil, err
		}
		rule := legacyRule("proxy", old.ID, "反代密码："+old.Name, true,
			[]Condition{legacyPath(old.LocationPath)}, Action{Type: "auth", AccountIDs: ids})
		rule.SourceDisabled = !old.Enabled
		policy.Rules = append(policy.Rules, rule)
		proxyAuthCount++
	}
	policy.Rules = append(policy.Rules, allowRules...)
	policy.Rules = append(policy.Rules, geoAllows...)
	if geoFallback != nil {
		policy.Rules = append(policy.Rules, *geoFallback)
	}
	if len(denyRules)+len(hotlinkRules)+len(authRules)+proxyAuthCount > 0 {
		policy.Warnings = append(policy.Warnings,
			"旧 location 最长前缀、正则及反代优先级无法等价转换为顺序规则。草稿先拒绝再验证密码；验证通过会结束检查。请核对重叠路径，原正则条件必须手动改为支持的条件后才能保存。")
	}
	return policy, nil
}

func legacyRule(sourceType, sourceID, name string, enabled bool, conditions []Condition, action Action) Rule {
	return Rule{ID: "legacy_" + sourceType + "_" + sourceID, Name: name, Enabled: enabled,
		Match: "all", Conditions: conditions, Action: action, SourceType: sourceType, SourceID: sourceID}
}

func legacyCSV(raw string) []string {
	values := []string{}
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func legacyPath(raw string) Condition {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "= ") {
		return Condition{Kind: "path", Operator: "exact", Values: []string{strings.TrimSpace(raw[2:])}}
	}
	if strings.HasPrefix(raw, "^~ ") {
		raw = strings.TrimSpace(raw[3:])
	}
	return Condition{Kind: "path", Operator: "prefix", Values: []string{raw}}
}

func legacyStringList(raw, kind, name string) ([]string, error) {
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("旧%s规则 %q 数据无效: %w", kind, name, err)
	}
	if values == nil {
		return nil, fmt.Errorf("旧%s规则 %q 必须是列表", kind, name)
	}
	return values, nil
}
