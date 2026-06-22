package nginxconf

import (
	"fmt"
	"regexp"
	"strings"
)

type DirectiveContext string

const (
	ContextMain   DirectiveContext = "main"
	ContextEvents DirectiveContext = "events"
	ContextHTTP   DirectiveContext = "http"
)

type ParameterDef struct {
	Key         string           `json:"key"`
	Context     DirectiveContext `json:"context"`
	Group       string           `json:"group"`
	Description string           `json:"description"`
	Tooltip     string           `json:"tooltip"`
	Default     string           `json:"default_value"`
	Unit        string           `json:"unit"`
	Options     []string         `json:"options,omitempty"`
	Clearable   bool             `json:"clearable,omitempty"`
}

var ParameterDefs = []ParameterDef{
	// 进程与连接
	{Key: "worker_processes", Context: ContextMain, Group: "进程与连接",
		Description: "处理进程，auto表示自动，数字表示进程数",
		Tooltip: "设置 Nginx 工作进程数量。设为 auto 时自动匹配 CPU 核心数，通常是最优选择。高并发场景建议保持 auto；若需限制资源占用可设为具体数字。",
		Default: "auto", Unit: ""},
	{Key: "worker_connections", Context: ContextEvents, Group: "进程与连接",
		Description: "最大并发链接数",
		Tooltip: "每个 worker 进程能同时处理的最大连接数。理论最大并发 = worker_processes × worker_connections。普通 Web 站点建议 1024-4096；高并发/长连接场景可设为 65535。",
		Default: "1024", Unit: ""},
	{Key: "worker_rlimit_nofile", Context: ContextMain, Group: "进程与连接",
		Description: "每个 worker 进程最大打开文件数",
		Tooltip: "每个 worker 进程能打开的最大文件描述符数量。高并发场景（大量连接或代理）需要调大，建议设为 65535 或更高。需配合系统 ulimit 设置。",
		Default: "1024", Unit: ""},
	{Key: "multi_accept", Context: ContextEvents, Group: "进程与连接",
		Description: "worker 一次接受所有新连接",
		Tooltip: "开启后 worker 进程一次 accept 所有等待中的新连接，而不是一次一个。高并发场景建议开启，可减少连接建立延迟。",
		Default: "off", Unit: ""},
	{Key: "use", Context: ContextEvents, Group: "进程与连接",
		Description: "事件驱动模型",
		Tooltip: "指定事件驱动模型。设为 auto 则由 nginx 自动选择（推荐）。Linux 通常为 epoll，macOS/BSD 为 kqueue。一般无需手动设置。",
		Default: "", Unit: "", Options: []string{"epoll", "kqueue", "select", "poll"}},

	// 性能优化
	{Key: "sendfile", Context: ContextHTTP, Group: "性能优化",
		Description: "使用内核 sendfile 传输文件",
		Tooltip: "启用后使用操作系统内核的 sendfile() 系统调用传输静态文件，跳过用户空间拷贝，显著提升静态文件传输性能。提供静态文件服务时建议开启。",
		Default: "on", Unit: ""},
	{Key: "tcp_nopush", Context: ContextHTTP, Group: "性能优化",
		Description: "优化数据包发送",
		Tooltip: "与 sendfile 配合使用，将 HTTP 响应头和文件数据合并为一个数据包发送，减少网络开销。仅在 sendfile 开启时有效，建议同步开启。",
		Default: "on", Unit: ""},
	{Key: "tcp_nodelay", Context: ContextHTTP, Group: "性能优化",
		Description: "禁用 Nagle 算法",
		Tooltip: "禁用 Nagle 算法，数据立即发送而不等待更多数据凑包。可降低小数据包的传输延迟，适合实时应用（WebSocket、API、SSE）。建议开启。",
		Default: "on", Unit: ""},
	{Key: "reset_timedout_connection", Context: ContextHTTP, Group: "性能优化",
		Description: "超时后立即重置连接",
		Tooltip: "开启后在连接超时时立即发送 RST 包释放资源，而非正常的四次挥手。可减少 FIN_WAIT 状态的积压连接。可能影响客户端日志记录。",
		Default: "off", Unit: ""},
	{Key: "lingering_close", Context: ContextHTTP, Group: "性能优化",
		Description: "延迟关闭连接",
		Tooltip: "控制在关闭连接前是否等待客户端读取剩余数据。设为 off 可立即关闭，减少资源占用，但客户端可能收到未完成的响应。",
		Default: "on", Unit: ""},
	{Key: "lingering_timeout", Context: ContextHTTP, Group: "性能优化",
		Description: "延迟关闭等待时间",
		Tooltip: "延迟关闭期间等待客户端读取剩余数据的超时时间。超时后关闭连接。仅在 lingering_close 开启时有效。",
		Default: "5", Unit: "秒"},

	// 安全 / SSL
	{Key: "server_tokens", Context: ContextHTTP, Group: "安全 / SSL",
		Description: "是否暴露 Nginx 版本号",
		Tooltip: "设为 off 可隐藏 Nginx 版本号，避免攻击者通过版本号查找已知漏洞。生产环境强烈建议关闭。关闭后错误页面和 Server 响应头仅显示「nginx」。",
		Default: "on", Unit: ""},
	{Key: "ssl_protocols", Context: ContextHTTP, Group: "安全 / SSL",
		Description: "启用的 TLS 协议版本",
		Tooltip: "设置允许的 SSL/TLS 协议版本。生产环境强烈建议仅启用 TLSv1.2 和 TLSv1.3，禁用 TLSv1/TLSv1.1 以避免已知安全漏洞。取消所有勾选不会修改已有配置。",
		Default: "TLSv1.2 TLSv1.3", Unit: "",
		Options: []string{"TLSv1", "TLSv1.1", "TLSv1.2", "TLSv1.3"}},
	{Key: "ssl_prefer_server_ciphers", Context: ContextHTTP, Group: "安全 / SSL",
		Description: "优先使用服务器密码套件顺序",
		Tooltip: "开启后 SSL 握手时优先使用服务器端配置的密码套件顺序，而非客户端的顺序。安全建议开启，可确保使用更安全的加密算法。",
		Default: "off", Unit: ""},
	{Key: "ssl_session_cache", Context: ContextHTTP, Group: "安全 / SSL",
		Description: "SSL 会话缓存",
		Tooltip: "缓存 SSL 会话以加速后续连接的 SSL 握手。建议设为 shared:SSL:10m（10MB 共享缓存，约可存储 40000 个会话）。设为 off 禁用缓存。清空此字段将删除该指令，禁用 SSL 会话缓存。",
		Default: "off", Unit: "", Clearable: true},
	{Key: "ssl_session_timeout", Context: ContextHTTP, Group: "安全 / SSL",
		Description: "SSL 会话超时时间",
		Tooltip: "SSL 会话缓存的有效时间。超时后客户端需要重新进行完整握手。建议 1h-4h。仅在 ssl_session_cache 开启时有效。清空此字段将删除该指令，nginx 使用默认值 5m。",
		Default: "5m", Unit: "", Clearable: true},
	{Key: "ssl_stapling", Context: ContextHTTP, Group: "安全 / SSL",
		Description: "OCSP Stapling",
		Tooltip: "开启后 Nginx 主动获取并缓存证书的 OCSP 状态，附加在 SSL 握手响应中，减少客户端验证证书的延迟。需要证书颁发机构支持 OCSP。",
		Default: "off", Unit: ""},

	// Gzip 压缩
	{Key: "gzip", Context: ContextHTTP, Group: "Gzip 压缩",
		Description: "是否开启压缩传输",
		Tooltip: "开启后 Nginx 会对响应内容进行 gzip 压缩再传输，减少带宽占用、加快页面加载速度。建议生产环境开启。",
		Default: "off", Unit: ""},
	{Key: "gzip_vary", Context: ContextHTTP, Group: "Gzip 压缩",
		Description: "添加 Vary: Accept-Encoding 头",
		Tooltip: "在响应头中添加 Vary: Accept-Encoding，告知 CDN 和代理服务器根据 Accept-Encoding 头缓存不同版本。开启 gzip 时建议同步开启。",
		Default: "off", Unit: ""},
	{Key: "gzip_min_length", Context: ContextHTTP, Group: "Gzip 压缩",
		Description: "最小压缩文件大小",
		Tooltip: "当响应体小于此值时不进行压缩，因为压缩小文件的 CPU 开销可能大于节省的带宽。建议设为 20（KB）以上。单位与 Content-Length 头一致。",
		Default: "20", Unit: "KB"},
	{Key: "gzip_comp_level", Context: ContextHTTP, Group: "Gzip 压缩",
		Description: "压缩率(1-9)",
		Tooltip: "控制 gzip 压缩级别。1 最快但压缩率最低，9 最慢但压缩率最高。一般 Web 场景推荐 4-6，在压缩率和 CPU 开销之间取得平衡。",
		Default: "6", Unit: ""},
	{Key: "gzip_types", Context: ContextHTTP, Group: "Gzip 压缩",
		Description: "需要压缩的 MIME 类型",
		Tooltip: "指定除 text/html（始终压缩）外还需要压缩的 MIME 类型。常见类型：text/plain text/css application/javascript application/json application/xml text/xml image/svg+xml。用空格分隔多个类型。清空此字段将删除该指令，nginx 仅压缩 text/html。",
		Default: "", Unit: "", Clearable: true},

	// 请求限制
	{Key: "client_max_body_size", Context: ContextHTTP, Group: "请求限制",
		Description: "最大上传文件大小",
		Tooltip: "客户端请求体的最大允许大小，主要限制文件上传大小。超过此值返回 413 错误。常见设置：普通表单 2m、图片上传 10m、视频上传 100m+。设为 0 表示不限制。清空此字段将删除该指令，nginx 默认限制为 1m。",
		Default: "1m", Unit: "MB", Clearable: true},
	{Key: "client_header_buffer_size", Context: ContextHTTP, Group: "请求限制",
		Description: "客户端请求头 buffer 大小",
		Tooltip: "读取客户端请求头的缓冲区大小。如果请求头较大（如含很长的 Cookie），可能需要调大，否则会返回 400 错误。大多数情况默认值足够。清空此字段将删除该指令，nginx 使用默认值 1k。",
		Default: "1k", Unit: "KB", Clearable: true},
	{Key: "client_body_buffer_size", Context: ContextHTTP, Group: "请求限制",
		Description: "请求主体缓冲区大小",
		Tooltip: "读取客户端请求体的缓冲区大小。超过此值会将请求体临时写入磁盘文件，影响性能。处理大表单或 POST 请求时可适当调大。清空此字段将删除该指令，nginx 使用默认值 16k。",
		Default: "16k", Unit: "KB", Clearable: true},
	{Key: "server_names_hash_bucket_size", Context: ContextHTTP, Group: "请求限制",
		Description: "服务器名字 hash 表大小",
		Tooltip: "服务器名字的 hash 表桶大小。当域名很长或绑定域名数量很多时可能需要调大。出现「could not build the server_names_hash」错误时按提示调大。常用值：64、128、256。",
		Default: "64", Unit: ""},

	// 超时设置
	{Key: "keepalive_timeout", Context: ContextHTTP, Group: "超时设置",
		Description: "keepalive 连接超时时间",
		Tooltip: "保持连接（Keep-Alive）的超时时间，超时后服务器关闭连接。设为 0 可禁用 keepalive。建议 30-65 秒；高并发短请求场景可降低到 15-30 秒释放连接资源。",
		Default: "65", Unit: "秒"},
	{Key: "send_timeout", Context: ContextHTTP, Group: "超时设置",
		Description: "响应发送超时时间",
		Tooltip: "向客户端发送响应的超时时间。如果客户端在此时间内没有读取任何数据，Nginx 将关闭连接。网络状况差的客户端可能需要调大。",
		Default: "60", Unit: "秒"},
	{Key: "proxy_connect_timeout", Context: ContextHTTP, Group: "超时设置",
		Description: "代理连接超时",
		Tooltip: "与后端服务器建立连接（TCP 三次握手）的超时时间。不能超过 75 秒。后端服务器响应慢或网络延迟大时可适当调大。",
		Default: "60", Unit: "秒"},
	{Key: "proxy_read_timeout", Context: ContextHTTP, Group: "超时设置",
		Description: "代理读取超时",
		Tooltip: "从后端服务器读取响应的超时时间。如果后端处理慢（大数据查询、AI 推理、视频转码）需要调大此值，否则会返回 504 Gateway Timeout。SSE/长连接场景建议设为 300+。",
		Default: "60", Unit: "秒"},
	{Key: "proxy_send_timeout", Context: ContextHTTP, Group: "超时设置",
		Description: "代理发送超时",
		Tooltip: "向后端服务器发送请求体的超时时间。上传大文件到后端时可能需要调大。两次连续写操作之间的间隔超过此值则超时。",
		Default: "60", Unit: "秒"},

	// 日志配置
	{Key: "log_not_found", Context: ContextHTTP, Group: "日志配置",
		Description: "是否记录 404 错误",
		Tooltip: "控制是否在 error_log 中记录 404 未找到错误。静态文件较多的站点可关闭此选项以减少日志量和磁盘 I/O。",
		Default: "on", Unit: ""},
	{Key: "log_subrequest", Context: ContextHTTP, Group: "日志配置",
		Description: "是否记录子请求",
		Tooltip: "控制是否记录由 SSI 或 rewrite 等产生的子请求日志。通常关闭，调试时可开启以追踪内部请求流程。清空此字段将删除该指令，nginx 默认不记录子请求。",
		Default: "off", Unit: "", Clearable: true},

	// 错误处理
	{Key: "server_name_in_redirect", Context: ContextHTTP, Group: "错误处理",
		Description: "重定向时使用哪个 server_name",
		Tooltip: "开启后在相对路径重定向中使用 server_name 指令的值作为 Host。关闭则使用客户端请求头中的 Host。多域名站点建议关闭。清空此字段将删除该指令，nginx 默认使用请求头中的 Host。",
		Default: "off", Unit: "", Clearable: true},
	{Key: "ignore_invalid_headers", Context: ContextHTTP, Group: "错误处理",
		Description: "忽略无效请求头",
		Tooltip: "开启后忽略格式不正确的 HTTP 请求头。安全建议开启，可防止某些基于畸形请求头的攻击。关闭则拒绝包含无效头的请求（返回 400）。",
		Default: "on", Unit: ""},
	{Key: "underscores_in_headers", Context: ContextHTTP, Group: "错误处理",
		Description: "允许请求头含下划线",
		Tooltip: "控制 HTTP 请求头名称中是否允许下划线字符。默认关闭，因为下划线可能被用于混淆安全相关的头。API 场景可能需要开启。",
		Default: "off", Unit: ""},
}

func buildDirectivePattern() string {
	keys := make([]string, len(ParameterDefs))
	for i, d := range ParameterDefs {
		keys[i] = regexp.QuoteMeta(d.Key)
	}
	return strings.Join(keys, "|")
}

func ParseParameters(content string) map[string]string {
	result := make(map[string]string)
	for _, def := range ParameterDefs {
		result[def.Key] = findDirectiveValue(content, def)
	}
	return result
}

func findDirectiveValue(content string, def ParameterDef) string {
	block := extractContextBlock(content, def.Context)
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(def.Key) + `\s+([^;]+)\s*;`)
	matches := re.FindStringSubmatch(block)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	commentedRe := regexp.MustCompile(`(?m)^\s*#\s*` + regexp.QuoteMeta(def.Key) + `\s+([^;]+)\s*;`)
	matches = commentedRe.FindStringSubmatch(block)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return def.Default
}

func extractContextBlock(content string, ctx DirectiveContext) string {
	switch ctx {
	case ContextMain:
		eventsIdx := findBlockStart(content, "events")
		httpIdx := findBlockStart(content, "http")
		minIdx := len(content)
		if eventsIdx > 0 && eventsIdx < minIdx {
			minIdx = eventsIdx
		}
		if httpIdx > 0 && httpIdx < minIdx {
			minIdx = httpIdx
		}
		if minIdx < len(content) {
			return content[:minIdx]
		}
		return content
	case ContextEvents:
		return extractBlock(content, "events")
	case ContextHTTP:
		return extractBlock(content, "http")
	}
	return content
}

func findBlockStart(content string, blockName string) int {
	re := regexp.MustCompile(`(?m)^\s*` + blockName + `\s*\{`)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return -1
	}
	return loc[0]
}

func extractBlock(content string, blockName string) string {
	re := regexp.MustCompile(`(?m)^\s*` + blockName + `\s*\{`)
	loc := re.FindStringIndex(content)
	if loc == nil {
		return ""
	}
	start := loc[1]
	depth := 1
	i := start
	for i < len(content) && depth > 0 {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		i++
	}
	if depth != 0 {
		return content[start:]
	}
	return content[loc[0]:i]
}

func deleteDirective(content string, def ParameterDef) string {
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(def.Key) + `\s+[^;]+\s*;\s*\n?`)
	result := re.ReplaceAllString(content, "")
	result = regexp.MustCompile(`(\n\s*\n\s*\n)`).ReplaceAllString(result, "\n\n")
	return result
}

func ApplyParameters(content string, params map[string]string) (string, error) {
	result := content
	for _, def := range ParameterDefs {
		val, ok := params[def.Key]
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		if val == "" {
			continue
		}
		if val == "__clear__" {
			if !def.Clearable {
				continue
			}
			result = deleteDirective(result, def)
			continue
		}
		var err error
		result, err = applyDirective(result, def, val)
		if err != nil {
			return "", fmt.Errorf("修改 %s 失败: %w", def.Key, err)
		}
	}
	return result, nil
}

func applyDirective(content string, def ParameterDef, value string) (string, error) {
	block := extractContextBlock(content, def.Context)
	if block == "" && def.Context != ContextMain {
		content = ensureBlockExists(content, def.Context)
		block = extractContextBlock(content, def.Context)
	}

	activeRe := regexp.MustCompile(`(?m)^(\s*)` + regexp.QuoteMeta(def.Key) + `\s+[^;]+\s*;`)
	commentedRe := regexp.MustCompile(`(?m)^(\s*)#\s*` + regexp.QuoteMeta(def.Key) + `\s+[^;]+\s*;`)

	if activeRe.MatchString(block) {
		newContent := activeRe.ReplaceAllStringFunc(content, func(match string) string {
			return regexp.MustCompile(`^(\s*)`+regexp.QuoteMeta(def.Key)+`\s+[^;]+\s*;`).ReplaceAllString(match, "${1}"+def.Key+" "+value+";")
		})
		return newContent, nil
	}

	if commentedRe.MatchString(block) {
		newContent := commentedRe.ReplaceAllStringFunc(content, func(match string) string {
			return regexp.MustCompile(`^(\s*)#\s*`+regexp.QuoteMeta(def.Key)+`\s+[^;]+\s*;`).ReplaceAllString(match, "${1}"+def.Key+" "+value+";")
		})
		return newContent, nil
	}

	return insertDirective(content, def, value), nil
}

func ensureBlockExists(content string, ctx DirectiveContext) string {
	blockName := string(ctx)
	if findBlockStart(content, blockName) >= 0 {
		return content
	}
	switch blockName {
	case "events":
		return "events {\n    worker_connections 1024;\n}\n\n" + content
	case "http":
		return content + "\nhttp {\n    include       mime.types;\n    default_type  application/octet-stream;\n}\n"
	}
	return content
}

func insertDirective(content string, def ParameterDef, value string) string {
	if strings.ContainsAny(value, "\n\r") {
		value = strings.ReplaceAll(value, "\n", " ")
		value = strings.ReplaceAll(value, "\r", "")
	}
	line := "    " + def.Key + " " + value + ";"

	switch def.Context {
	case ContextMain:
		eventsIdx := findBlockStart(content, "events")
		lines := strings.Split(content, "\n")
		insertAt := 0
		inserted := false
		for i, l := range lines {
			trimmed := strings.TrimSpace(l)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			insertAt = i
			if eventsIdx > 0 {
				lineStart := 0
				for j := 0; j < i; j++ {
					lineStart += len(lines[j]) + 1
				}
				if lineStart >= eventsIdx {
					insertAt = i
					break
				}
			}
			break
		}
		if !inserted {
			result := make([]string, 0, len(lines)+1)
			if insertAt > 0 {
				result = append(result, lines[:insertAt]...)
			}
			result = append(result, line)
			result = append(result, lines[insertAt:]...)
			return strings.Join(result, "\n")
		}

	case ContextEvents:
		block := extractBlock(content, "events")
		if block == "" {
			return content
		}
		newBlock := strings.Replace(block, "{", "{\n"+line, 1)
		return strings.Replace(content, block, newBlock, 1)

	case ContextHTTP:
		block := extractBlock(content, "http")
		if block == "" {
			return content
		}
		newBlock := strings.Replace(block, "{", "{\n"+line, 1)
		return strings.Replace(content, block, newBlock, 1)
	}

	return content
}
