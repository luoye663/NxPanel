package nginxpatch

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const MaintenanceStart = "#NXPANEL-MAINTENANCE-START "
const MaintenanceEnd = "#NXPANEL-MAINTENANCE-END"

var maintenanceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// MaintenanceBlock runs before the original server rewrite program. Internal
// error-page redirects skip that program too, so existing return/rewrite rules
// cannot intercept the page. ACME requests retain their original locations.
func MaintenanceBlock(id, pagePath string) (string, error) {
	if !maintenanceID.MatchString(id) || !staticPath(pagePath) {
		return "", fmt.Errorf("invalid maintenance identity or page path")
	}
	uri := "/__nxpanel_maintenance_" + id
	return fmt.Sprintf(`
    %s%s
    if ($uri = %s) { break; }
    if ($uri ~ "^/\\.well-known/acme-challenge/") { break; }
    return 503;
    error_page 503 %s;
    error_page 401 403 404 405 500 502 504 %s;
    location = %s {
        internal;
        root %s;
        try_files /%s =503;
        types { }
        default_type text/html;
        charset utf-8;
        recursive_error_pages off;
        auth_basic off;
        satisfy any;
        allow all;
        limit_req_dry_run on;
        limit_conn_dry_run on;
        add_header Retry-After 300 always;
        add_header Cache-Control no-store always;
    }
    %s
`, MaintenanceStart, id, uri, uri, uri, uri, filepath.Dir(pagePath), filepath.Base(pagePath), MaintenanceEnd), nil
}

// StripMaintenance removes only complete, line-delimited panel blocks. It never
// restores a saved server snapshot, so intervening certificate updates survive.
func StripMaintenance(content string) (string, int, error) { return stripMaintenance(content, "") }

func stripMaintenance(content, id string) (string, int, error) {
	type span struct {
		start, end int
		id         string
	}
	var spans []span
	cursor := 0
	for {
		rel := strings.Index(content[cursor:], "\n    "+MaintenanceStart)
		if rel < 0 {
			break
		}
		start := cursor + rel
		endRel := strings.Index(content[start:], "\n    "+MaintenanceEnd+"\n")
		if endRel < 0 {
			return "", 0, fmt.Errorf("incomplete maintenance block")
		}
		end := start + endRel + len("\n    "+MaintenanceEnd+"\n")
		part := content[start:end]
		if strings.Count(part, MaintenanceStart) != 1 {
			return "", 0, fmt.Errorf("nested maintenance blocks")
		}
		lineEnd := strings.Index(part[1:], "\n") + 1
		blockID := strings.TrimPrefix(part[1:lineEnd], "    "+MaintenanceStart)
		if !maintenanceID.MatchString(blockID) {
			return "", 0, fmt.Errorf("invalid maintenance block identity")
		}
		spans = append(spans, span{start, end, blockID})
		cursor = end
	}
	check := content
	result := content
	count := 0
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		check = check[:s.start] + check[s.end:]
		if id == "" || id == s.id {
			result = result[:s.start] + result[s.end:]
			count++
		}
	}
	if strings.Contains(check, MaintenanceStart) || strings.Contains(check, MaintenanceEnd) {
		return "", 0, fmt.Errorf("malformed maintenance markers")
	}
	return result, count, nil
}

func HasMaintenance(content string) bool { return strings.Contains(content, MaintenanceStart) }

// Global OpenResty rewrite/access hooks can run before server rewrite guards.
// Inspect the actual expanded configuration too, not only the site's file.
func ValidateMaintenanceRuntime(dump string) error {
	if len(dump) > 32<<20 {
		return fmt.Errorf("runtime configuration exceeds inspection budget")
	}
	tokens, err := lex(dump)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		n := token.value
		if strings.Contains(n, "_by_lua") || strings.HasPrefix(n, "js_") || n == "auth_request" || n == "auth_jwt" || n == "auth_digest" {
			return fmt.Errorf("global scripted or custom authentication phases cannot be safely isolated for maintenance")
		}
	}
	return nil
}

func MaintenanceIncludePaths(content string) ([]string, error) {
	ds, err := parse(content)
	if err != nil {
		return nil, err
	}
	var check func([]directive) error
	check = func(nodes []directive) error {
		for _, d := range nodes {
			n := d.words[0].value
			if strings.Contains(n, "lua") || strings.Contains(n, "js_") || n == "auth_request" || n == "auth_jwt" || n == "auth_digest" {
				return fmt.Errorf("scripted request phases are not supported for maintenance")
			}
			if err := check(d.children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(ds); err != nil {
		return nil, err
	}
	return includePaths(ds)
}

// SetMaintenance selects every explicit HTTP server for this site's primary
// name, but refuses servers that would also affect unrecorded domains.
func SetMaintenance(content, primary string, domains []string, id, pagePath string, enabled bool) (string, error) {
	base, _, err := stripMaintenance(content, id)
	if err != nil {
		return "", err
	}
	if !enabled {
		return base, nil
	}
	block, err := MaintenanceBlock(id, pagePath)
	if err != nil {
		return "", err
	}
	ds, err := parse(base)
	if err != nil {
		return "", err
	}
	allowed := map[string]bool{primary: true}
	for _, d := range domains {
		allowed[d] = true
	}
	type insertion struct {
		offset int
		block  string
	}
	var offsets []insertion
	var walk func([]directive) error
	walk = func(nodes []directive) error {
		for _, d := range nodes {
			if d.block && len(d.words) == 1 && d.words[0].value == "http" {
				if err := walk(d.children); err != nil {
					return err
				}
				continue
			}
			if !d.block || len(d.words) != 1 || d.words[0].value != "server" {
				continue
			}
			var names []string
			for _, child := range d.children {
				if child.words[0].value == "server_name" && !child.block {
					for _, w := range child.words[1:] {
						names = append(names, w.value)
					}
				}
			}
			match := false
			for _, name := range names {
				if name == primary {
					match = true
				}
			}
			if !match {
				continue
			}
			for _, name := range names {
				if !allowed[name] || strings.ContainsAny(name, "*~$") {
					return fmt.Errorf("server contains names not uniquely owned by this site")
				}
			}
			var check func([]directive) error
			check = func(children []directive) error {
				for _, child := range children {
					n := child.words[0].value
					if strings.Contains(n, "lua") || strings.Contains(n, "js_") || n == "auth_request" || n == "auth_jwt" || n == "auth_digest" {
						return fmt.Errorf("scripted request phases are not supported for maintenance")
					}
					if err := check(child.children); err != nil {
						return err
					}
				}
				return nil
			}
			if err := check(d.children); err != nil {
				return err
			}
			hasACME := false
			for _, child := range d.children {
				if child.words[0].value != "location" {
					continue
				}
				for _, word := range child.words[1:] {
					if strings.Contains(word.value, "acme-challenge") {
						if len(child.words) != 3 || child.words[1].value != "^~" || child.words[2].value != "/.well-known/acme-challenge/" {
							return fmt.Errorf("ACME location cannot be uniquely preserved; an explicit ^~ challenge prefix is required")
						}
						hasACME = true
						var safeACME func([]directive) error
						safeACME = func(nodes []directive) error {
							for _, node := range nodes {
								if node.words[0].value == "include" || strings.HasSuffix(node.words[0].value, "_pass") {
									return fmt.Errorf("proxied ACME locations cannot be isolated from the business upstream")
								}
								for _, arg := range node.words[1:] {
									if strings.HasPrefix(arg.value, "@") {
										return fmt.Errorf("named ACME fallbacks cannot be isolated from the business upstream")
									}
								}
								if err := safeACME(node.children); err != nil {
									return err
								}
							}
							return nil
						}
						if err := safeACME(child.children); err != nil {
							return err
						}
					}
				}
			}
			localBlock := block
			if !hasACME {
				localBlock = strings.Replace(block, "\n    "+MaintenanceEnd, "\n    location ^~ /.well-known/acme-challenge/ { return 404; }\n    "+MaintenanceEnd, 1)
			}
			// Locate the opening brace from the lexer, not a quoted/comment brace.
			tokens, err := lex(base[d.start:d.end])
			if err != nil {
				return err
			}
			if len(tokens) < 2 || tokens[1].sigil != '{' {
				return fmt.Errorf("invalid server opening")
			}
			offsets = append(offsets, insertion{d.start + tokens[1].end, localBlock})
		}
		return nil
	}
	if err := walk(ds); err != nil {
		return "", err
	}
	if len(offsets) == 0 {
		return "", fmt.Errorf("no explicit server belongs to this site")
	}
	if strings.Contains(base, "/__nxpanel_maintenance_"+id) {
		return "", fmt.Errorf("reserved maintenance URI already exists")
	}
	for i := len(offsets) - 1; i >= 0; i-- {
		p := offsets[i].offset
		base = base[:p] + offsets[i].block + base[p:]
	}
	return base, nil
}

// ValidateMaintenanceWrite is an Agent-side last line of defense. A maintenance
// transition may change only its removable blocks; other writes must be exact
// certificate argument patches with unchanged maintenance blocks.
func ValidateMaintenanceWrite(old, new string, transition bool) error {
	if !HasMaintenance(old) && !HasMaintenance(new) {
		return nil
	}
	a, _, err := StripMaintenance(old)
	if err != nil {
		return err
	}
	b, _, err := StripMaintenance(new)
	if err != nil {
		return err
	}
	if transition {
		if a != b {
			return fmt.Errorf("maintenance transition changes business configuration")
		}
		return nil
	}
	// Compare the block bytes as well, not merely the stripped business config.
	// Find exactly the original argument spans and replace them in-place. This
	// proof does not permit changes to includes, listen, rewrite, or comments.
	oldTokens, err := lex(old)
	if err != nil {
		return err
	}
	newTokens, err := lex(new)
	if err != nil {
		return err
	}
	if len(oldTokens) != len(newTokens) {
		return fmt.Errorf("configuration is locked during maintenance")
	}
	patched := old
	var changes []struct {
		start, end int
		value      string
	}
	for i := range oldTokens {
		if oldTokens[i].value == newTokens[i].value && oldTokens[i].sigil == newTokens[i].sigil {
			continue
		}
		if i == 0 || (oldTokens[i-1].value != "ssl_certificate" && oldTokens[i-1].value != "ssl_certificate_key") || !staticPath(oldTokens[i].value) || !staticPath(newTokens[i].value) {
			return fmt.Errorf("only certificate references may change during maintenance")
		}
		changes = append(changes, struct {
			start, end int
			value      string
		}{oldTokens[i].start, oldTokens[i].end, new[newTokens[i].start:newTokens[i].end]})
	}
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		patched = patched[:c.start] + c.value + patched[c.end:]
	}
	if patched != new {
		return fmt.Errorf("configuration is locked during maintenance")
	}
	return nil
}
