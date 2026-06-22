package nginx

import (
	"bytes"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"text/template"
)

func Render(data *RenderData) (string, error) {
	if data.SiteID == "" {
		return "", fmt.Errorf("SiteID 不能为空")
	}
	if data.PrimaryDomain == "" {
		return "", fmt.Errorf("PrimaryDomain 不能为空")
	}

	if len(data.Bindings) > 0 {
		consolidateBindings(data)
	}

	return renderSingle(data)
}

func consolidateBindings(data *RenderData) {
	var allDomains []string
	seenDomain := make(map[string]bool)
	for _, b := range data.Bindings {
		domain := strings.TrimSpace(b.Domain)
		if domain != "" && !seenDomain[domain] {
			allDomains = append(allDomains, domain)
			seenDomain[domain] = true
		}
	}
	data.ServerNames = strings.Join(allDomains, " ")
}

func BuildListenBlock(data *RenderData) string {
	if len(data.Bindings) > 0 {
		return buildListenBlockFromBindings(data)
	}
	ds := ""
	if data.DefaultServer {
		ds = " default_server"
	}
	var buf strings.Builder
	fmt.Fprintf(&buf, "    listen %d%s;", data.HTTPPort, ds)
	if data.SSL != nil && data.SSL.Enabled {
		fmt.Fprintf(&buf, "\n    listen %d ssl%s;", data.HTTPSPort, ds)
	}
	return buf.String()
}

func buildListenBlockFromBindings(data *RenderData) string {
	seen := make(map[int]bool)
	for _, b := range data.Bindings {
		seen[b.Port] = true
	}

	if data.SSL != nil && data.SSL.Enabled {
		seen[data.HTTPSPort] = true
	}

	ports := make([]int, 0, len(seen))
	for p := range seen {
		ports = append(ports, p)
	}
	sort.Ints(ports)

	ds := ""
	if data.DefaultServer {
		ds = " default_server"
	}

	var buf strings.Builder
	for i, p := range ports {
		if i > 0 {
			buf.WriteString("\n")
		}
		if data.SSL != nil && data.SSL.Enabled && p == data.HTTPSPort {
			fmt.Fprintf(&buf, "    listen %d ssl%s;", p, ds)
		} else {
			fmt.Fprintf(&buf, "    listen %d%s;", p, ds)
		}
	}
	return buf.String()
}

func BuildSSLBlock(data *RenderData) string {
	if data.SSL == nil || !data.SSL.Enabled {
		return ""
	}
	return executeTemplate(GetTemplateStore().SSL, data.SSL)
}

func BuildForceHTTPSBlock(data *RenderData) string {
	if data.SSL == nil || !data.SSL.Enabled || !data.SSL.ForceHTTPS {
		return ""
	}
	return executeTemplate(GetTemplateStore().ForceHTTPS, nil)
}

func BuildLogBlock(data *RenderData) string {
	var buf strings.Builder
	if data.AccessLogEnabled {
		fmt.Fprintf(&buf, "    access_log %s;\n", data.AccessLogPath)
	} else {
		buf.WriteString("    access_log off;\n")
	}
	fmt.Fprintf(&buf, "    error_log %s;", data.ErrorLogPath)
	return buf.String()
}

func BuildMainLocation(data *RenderData) string {
	for _, p := range data.Proxies {
		if p.LocationPath == "/" && p.Enabled {
			return BuildProxyLocation(p)
		}
	}
	return ""
}

func BuildStaticLocation() string {
	ts := GetTemplateStore()
	return executeTemplate(ts.StaticLocation, nil)
}

// BuildExtraLocations 构建非 / 路径的代理 location 块
func BuildExtraLocations(data *RenderData) string {
	var buf strings.Builder
	for _, p := range data.Proxies {
		if p.LocationPath != "/" && p.Enabled {
			buf.WriteString(BuildProxyLocation(p))
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

func BuildProxyLocation(p *ProxyData) string {
	return executeTemplate(GetTemplateStore().ProxyLocation, p)
}

// BuildProxyCacheConf 构建 proxy-cache.conf 内容
func BuildProxyCacheConf(cachePath string) string {
	return executeTemplate(GetTemplateStore().ProxyCache, map[string]string{
		"CachePath": cachePath,
	})
}

func BuildServerNameBlock(serverNames string) string {
	return fmt.Sprintf("    server_name %s;\n", serverNames)
}

func BuildRootBlock(rootPath, indexFiles string) string {
	return fmt.Sprintf("    root %s;\n    index %s;\n", rootPath, indexFiles)
}

func BuildDocumentBlock(data DocumentData) string {
	var buf strings.Builder
	// autoindex 是 server 级默认值；未来按 location 精细控制时应单独设计，避免混入本 marker。
	if data.AutoindexEnabled {
		buf.WriteString("    autoindex on;\n")
		if data.AutoindexExactSize {
			buf.WriteString("    autoindex_exact_size on;\n")
		} else {
			buf.WriteString("    autoindex_exact_size off;\n")
		}
		if data.AutoindexLocaltime {
			buf.WriteString("    autoindex_localtime on;\n")
		} else {
			buf.WriteString("    autoindex_localtime off;\n")
		}
		format := data.AutoindexFormat
		if format == "" {
			format = "html"
		}
		fmt.Fprintf(&buf, "    autoindex_format %s;\n", format)
	} else {
		buf.WriteString("    autoindex off;\n")
	}
	if data.ErrorPage404 != "" {
		fmt.Fprintf(&buf, "    error_page 404 %s;\n", data.ErrorPage404)
	}
	if data.ErrorPage403 != "" {
		fmt.Fprintf(&buf, "    error_page 403 %s;\n", data.ErrorPage403)
	}
	return buf.String()
}

func BuildACMEChallengeBlock(data *RenderData) string {
	return "    location ^~ /.well-known/acme-challenge/ {\n" +
		"        default_type \"text/plain\";\n" +
		"        root " + data.RootPath + ";\n" +
		"    }"
}

func renderSingle(data *RenderData) (string, error) {
	data.ListenBlock = BuildListenBlock(data)
	data.SSLBlock = BuildSSLBlock(data)
	data.ForceHTTPSBlock = BuildForceHTTPSBlock(data)
	data.LogBlock = BuildLogBlock(data)
	data.DocumentBlock = BuildDocumentBlock(data.Document)
	data.MainLocation = BuildMainLocation(data)
	data.ExtraLocations = BuildExtraLocations(data)

	return ExecuteTemplateString(GetTemplateStore().Site, data)
}

func executeTemplate(tmpl *template.Template, v interface{}) string {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		slog.Error("模板执行失败", "template", tmpl.Name(), "error", err)
		return ""
	}
	return buf.String()
}

func ExecuteTemplateString(tmpl *template.Template, v interface{}) (string, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, v); err != nil {
		return "", fmt.Errorf("渲染模板 %s 失败: %w", tmpl.Name(), err)
	}
	return buf.String(), nil
}
