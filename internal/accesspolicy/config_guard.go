package accesspolicy

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

const configGuardMaxFiles = 256
const configGuardMaxDepth = 16
const configGuardMaxBytes = 64 << 20
const configGuardMaxStatements = 65536

var conflictingAccessDirectives = map[string]bool{
	"auth_basic": true, "auth_basic_user_file": true, "auth_request": true,
	"allow": true, "deny": true, "satisfy": true, "return": true,
}

type nginxConfigDetector interface {
	DetectNginx(context.Context, *agentclient.NginxDetectRequest) (*agentclient.NginxDetectResponse, error)
}
type nginxConfigLister interface {
	FilesList(context.Context, string) (*agentclient.FilesListResponse, error)
}

// SetNginxConfigPath is configured once during dependency initialization.
func (s *Service) SetNginxConfigPath(path string) { s.nginxConfigPath = path }

type configStatement struct {
	words    []string
	children []configStatement
	block    bool
}

type configGuard struct {
	service    *Service
	ctx        context.Context
	base       string
	files      int
	bytes      int
	statements int
	active     map[string]bool
	seen       map[string]bool
}

// CheckConfig rejects independent checks that native policy variables would
// override or that could finish a request before the ordered rules run.
func (s *Service) CheckConfig(ctx context.Context, site *repo.Site, main []byte) error {
	global := s.nginxConfigPath
	if global == "" {
		if detector, ok := s.agent.(nginxConfigDetector); ok {
			info, err := detector.DetectNginx(ctx, &agentclient.NginxDetectRequest{})
			if err != nil {
				return configGuardError("nginx.conf", "无法确定主配置路径: "+err.Error())
			}
			if info == nil || info.ConfPath == "" {
				return configGuardError("nginx.conf", "Agent 未返回主配置路径")
			}
			global = info.ConfPath
		}
	}
	if global != "" && !filepath.IsAbs(global) {
		return configGuardError(global, "主配置路径必须是绝对路径")
	}
	guard := &configGuard{service: s, ctx: ctx, active: map[string]bool{}, seen: map[string]bool{}}
	if global != "" {
		guard.base = filepath.Dir(global)
	}
	clean := stripOwnedAccessBlocks(main)
	if err := guard.scanBytes(site.ConfigPath, clean, "site", 0); err != nil {
		return err
	}
	if global != "" {
		return guard.scanFile(global, "main", 0)
	}
	return nil
}

func configGuardError(path, detail string) error {
	return app.ErrValidationFailedMsg("访问策略配置预检失败（"+path+"）："+detail, nil)
}

func stripOwnedAccessBlocks(main []byte) []byte {
	clean := main
	for _, name := range []string{nginx.MarkerNameAccessLimit, nginx.MarkerNameHotlink, nginx.MarkerNameForceHTTPS} {
		if patched, err := nginx.ReplaceMarkerBlock(clean, name, nil); err == nil {
			clean = patched
		}
	}
	return clean
}

// checkCustomAccess retains the local guard used by narrow callers and tests.
func checkCustomAccess(main []byte) error {
	guard := &configGuard{ctx: context.Background(), active: map[string]bool{}, seen: map[string]bool{}}
	statements, err := parseConfigStatements(string(stripOwnedAccessBlocks(main)))
	if err != nil {
		return configGuardError("站点配置", err.Error())
	}
	var walk func([]configStatement) error
	walk = func(items []configStatement) error {
		for _, item := range items {
			if err := guard.checkDirective("站点配置", item.words[0]); err != nil {
				return err
			}
			if err := walk(item.children); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(statements)
}

func (g *configGuard) scanFile(path, scope string, depth int) error {
	if depth > configGuardMaxDepth {
		return configGuardError(path, "include 嵌套超过 16 层")
	}
	path = filepath.Clean(path)
	key := scope + ":" + path
	if g.active[key] {
		return configGuardError(path, "include 存在循环引用")
	}
	if g.seen[key] {
		return nil
	}
	if g.files >= configGuardMaxFiles {
		return configGuardError(path, "检查文件超过 256 个")
	}
	if err := g.ctx.Err(); err != nil {
		return err
	}
	content, _, err := g.service.agent.ReadFile(g.ctx, path)
	if err != nil {
		return configGuardError(path, "无法读取 include: "+err.Error())
	}
	g.active[key] = true
	defer delete(g.active, key)
	if err := g.scanBytes(path, content, scope, depth); err != nil {
		return err
	}
	g.seen[key] = true
	return nil
}

func (g *configGuard) scanBytes(path string, content []byte, scope string, depth int) error {
	g.files++
	g.bytes += len(content)
	if g.files > configGuardMaxFiles || g.bytes > configGuardMaxBytes {
		return configGuardError(path, "检查文件超过 256 个或总内容超过 64 MiB")
	}
	items, err := parseConfigForScope(string(content), scope)
	if err != nil {
		return configGuardError(path, err.Error())
	}
	return g.walk(path, items, scope, depth)
}

func (g *configGuard) walk(path string, items []configStatement, scope string, depth int) error {
	for _, item := range items {
		g.statements++
		if g.statements > configGuardMaxStatements {
			return configGuardError(path, "配置指令总数超过检查上限")
		}
		name := item.words[0]
		if scope == "site" || scope == "http" {
			if err := g.checkDirective(path, name); err != nil {
				return err
			}
		}
		if name == "include" {
			if item.block || len(item.words) != 2 {
				return configGuardError(path, "include 语法无效")
			}
			paths, err := g.includePaths(path, item.words[1])
			if err != nil {
				return err
			}
			for _, included := range paths {
				if err := g.scanFile(included, scope, depth+1); err != nil {
					return err
				}
			}
		} else if item.block {
			if scope == "site" {
				if err := g.walk(path, item.children, scope, depth); err != nil {
					return err
				}
			} else if scope == "main" && name == "http" {
				if err := g.walk(path, item.children, "http", depth); err != nil {
					return err
				}
			}
			// Other servers, map/geo data and upstream blocks do not define
			// HTTP-level inherited access directives for this site's server.
		}
	}
	return nil
}

func (g *configGuard) checkDirective(path, name string) error {
	if conflictingAccessDirectives[name] {
		return configGuardError(path, "包含编排之外的访问指令或提前响应 "+name+"，请先处理冲突")
	}
	return nil
}

func (g *configGuard) includePaths(source, path string) ([]string, error) {
	if path == "" || strings.ContainsAny(path, "$\x00\r\n") {
		return nil, configGuardError(source, "无法安全解析动态 include "+path)
	}
	if !filepath.IsAbs(path) {
		if g.base == "" {
			return nil, configGuardError(source, "相对 include 需要明确 nginx.conf 路径: "+path)
		}
		path = filepath.Join(g.base, path)
	}
	path = filepath.Clean(path)
	if !strings.ContainsAny(path, "*?[") {
		return []string{path}, nil
	}
	dir, pattern := filepath.Dir(path), filepath.Base(path)
	if strings.ContainsAny(dir, "*?[") {
		return nil, configGuardError(source, "暂不支持目录包含通配符的 include: "+path)
	}
	if _, err := filepath.Match(pattern, ""); err != nil {
		return nil, configGuardError(source, "include 通配符无效: "+path)
	}
	lister, ok := g.service.agent.(nginxConfigLister)
	if !ok {
		return nil, configGuardError(source, "Agent 无法展开 include 通配符: "+path)
	}
	listing, err := lister.FilesList(g.ctx, dir)
	if err != nil || listing == nil {
		return nil, configGuardError(source, fmt.Sprintf("无法列出 include 目录 %s: %v", dir, err))
	}
	if len(listing.Entries) > 65536 {
		return nil, configGuardError(source, "include 目录条目过多")
	}
	paths := []string{}
	for _, entry := range listing.Entries {
		if filepath.Base(entry.Name) != entry.Name || entry.Name == "." || entry.Name == ".." {
			return nil, configGuardError(source, "Agent 返回无效 include 文件名")
		}
		matched, _ := filepath.Match(pattern, entry.Name)
		if matched {
			if entry.IsDir {
				return nil, configGuardError(source, "include 匹配目录: "+entry.Name)
			}
			paths = append(paths, filepath.Join(dir, entry.Name))
			if len(paths) > configGuardMaxFiles {
				return nil, configGuardError(source, "include 匹配文件超过 256 个")
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func parseConfigStatements(text string) ([]configStatement, error) {
	return parseConfigForScope(text, "site")
}

func parseConfigForScope(text, scope string) ([]configStatement, error) {
	i, nodes := 0, 0
	var parse func(int, bool, string) ([]configStatement, error)
	parse = func(depth int, nested bool, scope string) ([]configStatement, error) {
		if depth > 64 {
			return nil, fmt.Errorf("配置块嵌套超过 64 层")
		}
		items := []configStatement{}
		words := []string{}
		for {
			token, delimiter, err := nextConfigToken(text, &i)
			if err != nil {
				return nil, err
			}
			if delimiter == 0 {
				if token == "" {
					if nested || len(words) > 0 {
						return nil, fmt.Errorf("配置块或指令未结束")
					}
					return items, nil
				}
				words = append(words, token)
				continue
			}
			if delimiter == '}' {
				if !nested || len(words) > 0 {
					return nil, fmt.Errorf("意外的配置块结束")
				}
				return items, nil
			}
			if len(words) == 0 {
				return nil, fmt.Errorf("配置缺少指令名称")
			}
			item := configStatement{words: words, block: delimiter == '{'}
			words = nil
			if item.block {
				if scope == "site" {
					item.children, err = parse(depth+1, true, scope)
				} else if scope == "main" && item.words[0] == "http" {
					item.children, err = parse(depth+1, true, "http")
				} else {
					err = skipConfigBlock(text, &i)
				}
				if err != nil {
					return nil, err
				}
			}
			nodes++
			if nodes > configGuardMaxStatements {
				return nil, fmt.Errorf("配置指令数量超过检查上限")
			}
			items = append(items, item)
		}
	}
	return parse(0, false, scope)
}

func skipConfigBlock(text string, pos *int) error {
	depth := 1
	for depth > 0 {
		token, delimiter, err := nextConfigToken(text, pos)
		if err != nil {
			return err
		}
		if token == "" && delimiter == 0 {
			return fmt.Errorf("配置块未结束")
		}
		if delimiter == '{' {
			depth++
		}
		if delimiter == '}' {
			depth--
		}
		if depth > 64 {
			return fmt.Errorf("配置块嵌套超过 64 层")
		}
	}
	return nil
}

// nextConfigToken keeps quoted/escaped punctuation and ${variables} inside one
// word; structural delimiters are emitted only outside those constructs.
func nextConfigToken(text string, pos *int) (string, byte, error) {
	for *pos < len(text) {
		c := text[*pos]
		if strings.ContainsRune(" \t\r\n", rune(c)) {
			*pos++
			continue
		}
		if c == '#' {
			for *pos < len(text) && text[*pos] != '\n' {
				*pos++
			}
			continue
		}
		break
	}
	if *pos >= len(text) {
		return "", 0, nil
	}
	if c := text[*pos]; c == ';' || c == '{' || c == '}' {
		*pos++
		return "", c, nil
	}
	var word strings.Builder
	var quote byte
	started := false
	for *pos < len(text) {
		c := text[*pos]
		if c == '\\' {
			*pos++
			if *pos >= len(text) {
				return "", 0, fmt.Errorf("配置转义未结束")
			}
			word.WriteByte(text[*pos])
			*pos++
			started = true
			continue
		}
		if quote != 0 {
			*pos++
			if c == quote {
				quote = 0
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			*pos++
			started = true
			continue
		}
		if c == '$' && *pos+1 < len(text) && text[*pos+1] == '{' {
			end := strings.IndexByte(text[*pos+2:], '}')
			if end < 0 {
				return "", 0, fmt.Errorf("变量引用未结束")
			}
			end += *pos + 3
			word.WriteString(text[*pos:end])
			*pos = end
			started = true
			continue
		}
		if strings.ContainsRune(" \t\r\n;{}", rune(c)) {
			break
		}
		word.WriteByte(c)
		*pos++
		started = true
	}
	if quote != 0 {
		return "", 0, fmt.Errorf("配置字符串未结束")
	}
	if started && word.Len() == 0 {
		return "\x00empty", 0, nil
	}
	return word.String(), 0, nil
}
