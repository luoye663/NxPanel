package accessanalysis

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var nginxTimeLayouts = []string{
	"02/Jan/2006:15:04:05 -0700",
	time.RFC3339,
	"2006-01-02T15:04:05-07:00",
	"2006-01-02 15:04:05",
}

// Parser 负责把单行 access log 转成统一 Entry。
// 自定义正则只在初始化时编译一次，避免扫描热路径重复编译。
type Parser struct {
	format         LogFormat
	customPattern  string
	customRe       *regexp.Regexp
	normalizeQuery bool
}

func NewParser(format, customPattern string, normalizeQuery bool) (*Parser, error) {
	f := LogFormat(strings.TrimSpace(format))
	if f == "" {
		f = FormatCombined
	}
	p := &Parser{format: f, customPattern: customPattern, normalizeQuery: normalizeQuery}
	if f == FormatCustom {
		if len(customPattern) == 0 || len(customPattern) > 2048 {
			return nil, fmt.Errorf("自定义正则长度必须在 1-2048 字符之间")
		}
		re, err := regexp.Compile(customPattern)
		if err != nil {
			return nil, fmt.Errorf("自定义正则编译失败: %w", err)
		}
		missing := missingGroups(re, []string{"ip", "time", "method", "path", "status", "bytes"})
		if len(missing) > 0 {
			return nil, fmt.Errorf("自定义正则缺少命名字段: %s", strings.Join(missing, ", "))
		}
		p.customRe = re
	}
	return p, nil
}

func (p *Parser) ParseLine(line string) (Entry, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return Entry{}, fmt.Errorf("空日志行")
	}
	switch p.format {
	case FormatCommon, FormatCombined:
		return p.parseCommonCombined(line)
	case FormatNxpanelJSON:
		return p.parseNxpanelJSON(line)
	case FormatCustom:
		return p.parseCustom(line)
	default:
		return Entry{}, fmt.Errorf("不支持的日志格式: %s", p.format)
	}
}

func (p *Parser) parseCommonCombined(line string) (Entry, error) {
	open := strings.IndexByte(line, '[')
	close := strings.IndexByte(line, ']')
	firstQuote := strings.IndexByte(line, '"')
	if open <= 0 || close <= open || firstQuote <= close {
		return Entry{}, fmt.Errorf("common/combined 格式不完整")
	}

	ip := firstField(line[:open])
	ts, err := parseLogTime(line[open+1 : close])
	if err != nil {
		return Entry{}, err
	}

	request, afterReq, err := takeQuoted(line[firstQuote:])
	if err != nil {
		return Entry{}, err
	}
	parts := strings.Fields(request)
	if len(parts) < 2 {
		return Entry{}, fmt.Errorf("request 字段不完整")
	}

	rest := strings.Fields(afterReq)
	if len(rest) < 2 {
		return Entry{}, fmt.Errorf("状态码或流量字段缺失")
	}
	status, _ := strconv.Atoi(rest[0])
	bytesSent := parseBytes(rest[1])

	entry := Entry{
		TS:      ts.Format(time.RFC3339),
		IP:      truncateUTF8(ip, MaxIPBytes),
		Method:  truncateUTF8(parts[0], MaxMethodBytes),
		RawPath: truncateUTF8(parts[1], MaxPathBytes),
		Path:    p.normalizePath(parts[1]),
		Status:  status,
		Bytes:   bytesSent,
	}

	// combined 在 common 之后追加 referer 和 user_agent 两个引号字段。
	if p.format == FormatCombined {
		if referer, tail, err := takeNextQuoted(afterReq); err == nil {
			entry.Referer = truncateUTF8(emptyDash(referer), MaxRefererBytes)
			if ua, _, err := takeNextQuoted(tail); err == nil {
				entry.UserAgent = truncateUTF8(emptyDash(ua), MaxUserAgentBytes)
			}
		}
	}

	markEntryAnomaly(&entry)
	return entry, nil
}

func (p *Parser) parseNxpanelJSON(line string) (Entry, error) {
	var raw struct {
		Time    string `json:"time"`
		IP      string `json:"ip"`
		Method  string `json:"method"`
		URI     string `json:"uri"`
		Path    string `json:"path"`
		Status  int    `json:"status"`
		Bytes   int64  `json:"bytes"`
		Referer string `json:"referer"`
		UA      string `json:"ua"`
	}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return Entry{}, fmt.Errorf("JSON 日志解析失败: %w", err)
	}
	ts, err := parseLogTime(raw.Time)
	if err != nil {
		return Entry{}, err
	}
	rawPath := raw.URI
	if rawPath == "" {
		rawPath = raw.Path
	}
	entry := Entry{TS: ts.Format(time.RFC3339), IP: truncateUTF8(raw.IP, MaxIPBytes), Method: truncateUTF8(raw.Method, MaxMethodBytes), RawPath: truncateUTF8(rawPath, MaxPathBytes), Path: p.normalizePath(rawPath), Status: raw.Status, Bytes: raw.Bytes, Referer: truncateUTF8(emptyDash(raw.Referer), MaxRefererBytes), UserAgent: truncateUTF8(emptyDash(raw.UA), MaxUserAgentBytes)}
	markEntryAnomaly(&entry)
	return entry, nil
}

func (p *Parser) parseCustom(line string) (Entry, error) {
	match := p.customRe.FindStringSubmatch(line)
	if match == nil {
		return Entry{}, fmt.Errorf("自定义正则未匹配")
	}
	values := map[string]string{}
	for i, name := range p.customRe.SubexpNames() {
		if i > 0 && name != "" {
			values[name] = match[i]
		}
	}
	ts, err := parseLogTime(values["time"])
	if err != nil {
		return Entry{}, err
	}
	status, _ := strconv.Atoi(values["status"])
	entry := Entry{TS: ts.Format(time.RFC3339), IP: truncateUTF8(values["ip"], MaxIPBytes), Method: truncateUTF8(values["method"], MaxMethodBytes), RawPath: truncateUTF8(values["path"], MaxPathBytes), Path: p.normalizePath(values["path"]), Status: status, Bytes: parseBytes(values["bytes"]), Referer: truncateUTF8(emptyDash(values["referer"]), MaxRefererBytes), UserAgent: truncateUTF8(emptyDash(values["user_agent"]), MaxUserAgentBytes)}
	markEntryAnomaly(&entry)
	return entry, nil
}

func (p *Parser) normalizePath(raw string) string {
	if raw == "" {
		return "/"
	}
	path := raw
	if parsed, err := url.ParseRequestURI(raw); err == nil && parsed.Path != "" {
		path = parsed.Path
		if !p.normalizeQuery && parsed.RawQuery != "" {
			path += "?" + parsed.RawQuery
		}
	} else if p.normalizeQuery {
		if idx := strings.IndexByte(path, '?'); idx >= 0 {
			path = path[:idx]
		}
	}
	return truncateUTF8(path, MaxPathBytes)
}

func parseLogTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range nginxTimeLayouts {
		if ts, err := time.Parse(layout, value); err == nil {
			return ts.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("无法解析日志时间: %s", value)
}

func firstField(value string) string {
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func takeQuoted(value string) (string, string, error) {
	if !strings.HasPrefix(value, "\"") {
		return "", value, fmt.Errorf("缺少引号字段")
	}
	for i := 1; i < len(value); i++ {
		if value[i] == '"' && value[i-1] != '\\' {
			return value[1:i], value[i+1:], nil
		}
	}
	return "", value, fmt.Errorf("引号字段未闭合")
}

func takeNextQuoted(value string) (string, string, error) {
	idx := strings.IndexByte(value, '"')
	if idx < 0 {
		return "", value, fmt.Errorf("未找到下一个引号字段")
	}
	return takeQuoted(value[idx:])
}

func parseBytes(value string) int64 {
	if value == "-" || value == "" {
		return 0
	}
	n, _ := strconv.ParseInt(value, 10, 64)
	return n
}

func emptyDash(value string) string {
	if value == "-" {
		return ""
	}
	return value
}

func missingGroups(re *regexp.Regexp, required []string) []string {
	found := map[string]bool{}
	for _, name := range re.SubexpNames() {
		found[name] = true
	}
	var missing []string
	for _, name := range required {
		if !found[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func markEntryAnomaly(entry *Entry) {
	path := strings.ToLower(entry.Path)
	ua := strings.ToLower(entry.UserAgent)
	if entry.Status >= 500 {
		entry.IsAnomaly = true
		entry.AnomalyReason = "5xx 响应"
		return
	}
	if entry.Status == 404 && (strings.Contains(path, "/wp-") || strings.Contains(path, ".env") || strings.Contains(path, "phpmyadmin")) {
		entry.IsAnomaly = true
		entry.AnomalyReason = "疑似扫描器路径"
		return
	}
	if strings.Contains(ua, "sqlmap") || strings.Contains(ua, "nikto") || strings.Contains(ua, "masscan") {
		entry.IsAnomaly = true
		entry.AnomalyReason = "异常 UA"
	}
}
