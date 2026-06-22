// parser 包 — nginx -T 输出解析器
//
// 解析 nginx -T 的输出，提取 server block 信息。
// 用于站点导入扫描功能。
//
// nginx -T 输出格式：
//
//	# configuration file /etc/nginx/conf.d/legacy.conf:
//	server {
//	    listen 80;
//	    server_name old.example.com;
//	    root /var/www/old;
//	    ...
//	}
//
// 解析逻辑：
//  1. 识别 "# configuration file xxx:" 行获取来源文件
//  2. 提取 server { ... } 块
//  3. 从 server block 中解析 listen、server_name、root、access_log、error_log
package parser

import (
	"regexp"
	"strings"
)

var configLineRe = regexp.MustCompile(`^#\s*configuration file\s+(.+):\s*$`)

type ParsedServer struct {
	SourceFile    string
	Listen        []string
	ServerNames   []string
	RootPath      string
	AccessLogPath string
	ErrorLogPath  string
	RawBlock      string
}

func ParseNginxDump(output string) []*ParsedServer {
	var servers []*ParsedServer

	lines := strings.Split(output, "\n")
	currentFile := ""
	i := 0

	for i < len(lines) {
		line := lines[i]

		if matches := configLineRe.FindStringSubmatch(line); len(matches) == 2 {
			currentFile = strings.TrimSpace(matches[1])
			i++
			continue
		}

		trimmed := strings.TrimSpace(line)
		if isServerKeyword(trimmed) {
			block, endIdx := extractServerBlock(lines, i)
			if block != "" {
				servers = append(servers, parseServerBlock(block, currentFile))
			}
			i = endIdx + 1
			continue
		}

		i++
	}

	return servers
}

func isServerKeyword(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "server") {
		return false
	}
	rest := trimmed[len("server"):]
	if len(rest) == 0 {
		return true
	}
	return rest[0] == ' ' || rest[0] == '\t' || rest[0] == '{'
}

func extractServerBlock(lines []string, startIndex int) (string, int) {
	braceCount := 0
	started := false
	var blockLines []string

	for i := startIndex; i < len(lines); i++ {
		line := lines[i]
		blockLines = append(blockLines, line)

		for _, ch := range line {
			if ch == '{' {
				braceCount++
				started = true
			} else if ch == '}' {
				braceCount--
			}
		}

		if started && braceCount == 0 {
			return strings.Join(blockLines, "\n"), i
		}
	}

	return "", startIndex
}

func parseServerBlock(block, sourceFile string) *ParsedServer {
	s := &ParsedServer{
		SourceFile: sourceFile,
		RawBlock:   block,
	}

	lines := strings.Split(block, "\n")
	depth := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			for _, ch := range trimmed {
				switch ch {
				case '{':
					depth++
				case '}':
					if depth > 0 {
						depth--
					}
				}
			}
			continue
		}

		directive, args := parseDirective(trimmed)
		if directive == "" {
			for _, ch := range trimmed {
				switch ch {
				case '{':
					depth++
				case '}':
					if depth > 0 {
						depth--
					}
				}
			}
			continue
		}

		switch directive {
		case "listen":
			if len(args) > 0 {
				s.Listen = append(s.Listen, args[0])
			}
		case "server_name":
			s.ServerNames = append(s.ServerNames, args...)
		case "root":
			if len(args) > 0 {
				s.RootPath = args[0]
			}
		case "access_log":
			if depth == 1 && len(args) > 0 {
				s.AccessLogPath = args[0]
			}
		case "error_log":
			if depth == 1 && len(args) > 0 {
				s.ErrorLogPath = args[0]
			}
		}

		for _, ch := range trimmed {
			switch ch {
			case '{':
				depth++
			case '}':
				if depth > 0 {
					depth--
				}
			}
		}
	}

	return s
}

// ExtractServerLogPaths parses raw nginx config file content and returns
// the server-level access_log and error_log paths from the first server block.
// Returns empty strings if not found.
func ExtractServerLogPaths(content string) (accessLog, errorLog string) {
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if isServerKeyword(trimmed) {
			block, endIdx := extractServerBlock(lines, i)
			if block != "" {
				s := parseServerBlock(block, "")
				return s.AccessLogPath, s.ErrorLogPath
			}
			i = endIdx
		}
	}
	return "", ""
}

func parseDirective(line string) (string, []string) {
	line = strings.TrimSuffix(line, ";")
	line = strings.TrimSpace(line)

	parts := strings.Fields(line)
	if len(parts) == 0 {
		return "", nil
	}

	return parts[0], parts[1:]
}
