package accesspolicy

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Compile generates an http-context include and a server-context include. Maps
// are deliberately non-volatile: the first server rewrite evaluates the decision
// using the original normalized URI, and internal redirects retain that decision.
func Compile(siteID, panelDir string, p Policy, ctx RenderContext) (*CompiledPolicy, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(panelDir) || strings.ContainsAny(panelDir, "\x00\r\n$") {
		return nil, fmt.Errorf("策略配置目录必须为绝对路径")
	}
	suffix := fmt.Sprintf("%x", sha256.Sum256([]byte(siteID)))[:16]
	base := "$nx_policy_" + suffix
	result := &CompiledPolicy{Files: map[string]string{}}
	var global, server strings.Builder
	fmt.Fprintf(&global, "# Ordered access policy for site %s\n", suffix)
	fmt.Fprintf(&global, "geo %s_dollar { default \"$\"; }\n", base)
	needsCountry := false
	for _, r := range p.Rules {
		if r.Enabled && !r.SourceDisabled {
			for _, c := range r.Conditions {
				if c.Kind == "country" {
					needsCountry = true
				}
			}
		}
	}
	if needsCountry {
		fmt.Fprintf(&global, "geo %s_country {\n    default ZZ;\n", base)
		keys := make([]string, 0, len(ctx.CountryNetworks))
		for k := range ctx.CountryNetworks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		networks := map[string]string{}
		for _, k := range keys {
			code := strings.ToUpper(k)
			if err := validateCondition(Condition{Kind: "country", Values: []string{code}}); err != nil {
				return nil, err
			}
			for _, v := range ctx.CountryNetworks[k] {
				net, err := parseNetwork(v)
				if err != nil {
					return nil, fmt.Errorf("地域数据包含无效网络: %w", err)
				}
				network := net.String()
				if previous, ok := networks[network]; ok {
					if previous != code {
						return nil, fmt.Errorf("地域数据包含冲突网络 %s", network)
					}
					continue
				}
				networks[network] = code
				fmt.Fprintf(&global, "    %s %s;\n", network, code)
				if global.Len() > MaxRenderedBytes {
					return nil, fmt.Errorf("生成配置超过 64 MiB")
				}
			}
		}
		global.WriteString("}\n")
	}
	active := []Rule{}
	for _, r := range p.Rules {
		if r.Enabled && !r.SourceDisabled {
			active = append(active, r)
		}
	}
	for i, r := range active {
		vars := []string{}
		for j, c := range r.Conditions {
			c = expandServerNames(c, ctx.ServerNames)
			if len(c.Values) == 0 {
				return nil, fmt.Errorf("Referer server_names 未解析为有效域名")
			}
			variable := fmt.Sprintf("%s_r%d_c%d", base, i, j)
			vars = append(vars, variable)
			if err := renderCondition(&global, variable, base, c); err != nil {
				return nil, err
			}
			if global.Len() > MaxRenderedBytes {
				return nil, fmt.Errorf("生成配置超过 64 MiB")
			}
		}
		ruleVar := fmt.Sprintf("%s_r%d", base, i)
		if len(vars) == 0 {
			v := 0
			if r.Match == "all" {
				v = 1
			}
			fmt.Fprintf(&global, "map $host %s { default %d; }\n", ruleVar, v)
			continue
		}
		fmt.Fprintf(&global, "map %s %s {\n    default 0;\n", quote(strings.Join(vars, "")), ruleVar)
		pattern := "1"
		if r.Match == "all" {
			pattern = "^" + strings.Repeat("1", len(vars)) + "$"
		}
		fmt.Fprintf(&global, "    %s 1;\n}\n", quote("~"+pattern))
	}
	vars := []string{}
	for i := range active {
		vars = append(vars, fmt.Sprintf("%s_r%d", base, i))
	}
	if len(vars) == 0 {
		vars = []string{"$host"}
	}
	fmt.Fprintf(&global, "map %s %s_decision {\n    default default;\n", quote(strings.Join(vars, "")), base)
	for i, r := range active {
		fmt.Fprintf(&global, "    %s %s;\n", quote(fmt.Sprintf("~^.{%d}1", i)), r.ID)
	}
	global.WriteString("}\n")
	// Force evaluation before any site/content rewrite. The underlying decision map
	// is cached even when this server-level set executes again on an internal redirect.
	fmt.Fprintf(&server, "set %s_frozen %s_decision;\n", base, base)
	all := append(append([]Rule{}, active...), Rule{ID: "default", Action: p.DefaultAction})
	authPaths := map[string]string{}
	targets := map[string]string{}
	hasAuth := false
	for slot, r := range all {
		switch r.Action.Type {
		case "auth":
			body, ok := ctx.AuthFiles[r.ID]
			if !ok || strings.TrimSpace(body) == "" {
				return nil, fmt.Errorf("规则 %s 的验证账户尚未解析", r.ID)
			}
			if len(body) > 1024*1024 || strings.ContainsRune(body, 0) {
				return nil, fmt.Errorf("验证账户文件无效或过大")
			}
			if r.ID == "default" {
				slot = MaxRules
			}
			file := AuthSlotPaths(siteID, panelDir)[slot]
			authPaths[r.ID] = file
			result.Files[file] = body
			hasAuth = true
		case "deny":
			if effectiveStatus(r.Action) == 444 {
				fmt.Fprintf(&server, "if (%s_frozen = %s) { return 444; }\n", base, r.ID)
				continue
			}
			targets[r.ID] = "/__nxpanel_policy_" + suffix + "/" + r.ID
		}
	}
	if hasAuth {
		fmt.Fprintf(&global, "map %s_decision %s_realm {\n    default off;\n", base, base)
		for _, r := range all {
			if r.Action.Type == "auth" {
				fmt.Fprintf(&global, "    %s \"Restricted\";\n", quote("~\\A"+regexp.QuoteMeta(r.ID)+"\\z"))
			}
		}
		global.WriteString("}\n")
		fmt.Fprintf(&global, "map %s_decision %s_auth_file {\n    default %s;\n", base, base, quote(filepath.Join(panelDir, "access-policy", suffix+"-unused")))
		for _, r := range all {
			if file, ok := authPaths[r.ID]; ok {
				fmt.Fprintf(&global, "    %s %s;\n", quote("~\\A"+regexp.QuoteMeta(r.ID)+"\\z"), quote(file))
			}
		}
		global.WriteString("}\n")
		fmt.Fprintf(&server, "auth_basic %s_realm;\nauth_basic_user_file %s_auth_file;\n", base, base)
	} else {
		server.WriteString("auth_basic off;\n")
	}
	if len(targets) > 0 {
		fmt.Fprintf(&global, "map %s_decision %s_target {\n    default \"\";\n", base, base)
		for _, r := range all {
			if target, ok := targets[r.ID]; ok {
				fmt.Fprintf(&global, "    %s %s;\n", quote("~\\A"+regexp.QuoteMeta(r.ID)+"\\z"), quote(target))
			}
		}
		global.WriteString("}\n")
		// Only this guard is volatile, because it must notice arriving at the private
		// response URI. It never changes the original decision or authentication.
		fmt.Fprintf(&global, "map $uri %s_redirect {\n    volatile;\n    default %s_target;\n", base, base)
		for _, r := range all {
			if target, ok := targets[r.ID]; ok {
				fmt.Fprintf(&global, "    %s \"\";\n", quote("~\\A"+regexp.QuoteMeta(target)+"\\z"))
			}
		}
		global.WriteString("}\n")
		fmt.Fprintf(&server, "if (%s_redirect) { rewrite ^ %s_redirect last; }\n", base, base)
		for _, r := range all {
			if target, ok := targets[r.ID]; ok {
				mime := "text/plain"
				if r.Action.ResponseType == "html" {
					mime = "text/html"
				}
				body := r.Action.ResponseBody
				if body == "" {
					body = "Access denied\n"
				}
				body = strings.ReplaceAll(body, "$", "${"+strings.TrimPrefix(base, "$")+"_dollar}")
				fmt.Fprintf(&server, "location = %s {\n    internal;\n    auth_basic off;\n    default_type %s;\n    return %d %s;\n}\n", quote(target), mime, effectiveStatus(r.Action), quote(body))
			}
		}
	}
	result.GlobalContent = global.String()
	result.ServerContent = server.String()
	size := len(result.GlobalContent) + len(result.ServerContent)
	for _, v := range result.Files {
		size += len(v)
	}
	if size > MaxRenderedBytes {
		return nil, fmt.Errorf("生成配置超过 64 MiB")
	}
	return result, nil
}
func renderCondition(b *strings.Builder, variable, base string, c Condition) error {
	yes, no := 1, 0
	if c.Negate {
		yes, no = 0, 1
	}
	if c.Kind == "ip" {
		fmt.Fprintf(b, "geo %s {\n    default %d;\n", variable, no)
		seen := map[string]bool{}
		for _, v := range c.Values {
			p, _ := parseNetwork(v)
			if !seen[p.String()] {
				fmt.Fprintf(b, "    %s %d;\n", p, yes)
				seen[p.String()] = true
				if b.Len() > MaxRenderedBytes {
					return fmt.Errorf("生成配置超过 64 MiB")
				}
			}
		}
		b.WriteString("}\n")
		return nil
	}
	source := "$uri"
	if c.Kind == "country" {
		source = base + "_country"
	}
	if c.Kind == "referer" {
		source = "$http_referer"
	}
	blocked := false
	for _, v := range c.Values {
		if c.Kind == "referer" && v == "blocked" {
			blocked = true
		}
	}
	fallback := no
	if blocked {
		fallback = yes
	}
	fmt.Fprintf(b, "map %s %s {\n    default %d;\n", source, variable, fallback)
	seen := map[string]bool{}
	for _, v := range c.Values {
		pattern := ""
		switch c.Kind {
		case "country":
			pattern = strings.ToUpper(v)
		case "path":
			pattern = "~\\A" + regexp.QuoteMeta(v)
			if c.Operator == "exact" {
				pattern += "\\z"
			}
		case "extension":
			pattern = "~*\\." + regexp.QuoteMeta(strings.TrimPrefix(v, ".")) + "\\z"
		case "referer":
			if v == "blocked" {
				continue
			}
			expr, err := refererPattern(v)
			if err != nil {
				return err
			}
			pattern = "~*" + expr
		}
		if !seen[pattern] {
			fmt.Fprintf(b, "    %s %d;\n", quote(pattern), yes)
			seen[pattern] = true
			if b.Len() > MaxRenderedBytes {
				return fmt.Errorf("生成配置超过 64 MiB")
			}
		}
	}
	if blocked {
		fmt.Fprintf(b, "    ~*^https?:// %d;\n", no)
		if !seen["~*\\A\\z"] {
			fmt.Fprintf(b, "    \"\" %d;\n", no)
		}
	}
	b.WriteString("}\n")
	return nil
}
func quote(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }

func expandServerNames(c Condition, names []string) Condition {
	if c.Kind != "referer" {
		return c
	}
	values := []string{}
	for _, v := range c.Values {
		if v == "server_names" {
			for _, name := range names {
				if _, err := refererPattern(name); err == nil && name != "server_names" {
					values = append(values, name)
				}
			}
		} else {
			values = append(values, v)
		}
	}
	c.Values = values
	return c
}

// AuthSlotPaths is stable across rule edits, so backup archive positions never
// depend on the current rule IDs, their order, or the number of auth rules.
func AuthSlotPaths(siteID, panelDir string) []string {
	suffix := fmt.Sprintf("%x", sha256.Sum256([]byte(siteID)))[:16]
	paths := make([]string, MaxRules+1)
	for i := range paths {
		paths[i] = filepath.Join(panelDir, "access-policy", fmt.Sprintf("%s-slot-%d.htpasswd", suffix, i))
	}
	return paths
}
