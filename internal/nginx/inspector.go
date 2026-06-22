package nginx

import (
	"os"
	"strings"
)

// 已迁移到 markers_constants.go: MarkerIncludeStart / MarkerIncludeEnd

type IncludeEntryData struct {
	ConfDDir       string
	SitesEnabledDir string
}

func RenderIncludeEntry(panelDir string) string {
	data := IncludeEntryData{
		ConfDDir:       panelDir + "/conf.d",
		SitesEnabledDir: panelDir + "/sites-enabled",
	}
	return executeTemplate(GetTemplateStore().IncludeEntry, data)
}

func CheckIncludeInstalled(filePath string) bool {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, MarkerIncludeStart) &&
		strings.Contains(content, MarkerIncludeEnd)
}

func InsertIncludeInHTTPBlock(content string, panelDir string) (string, error) {
	httpIdx := findHTTPBlock(content)
	if httpIdx < 0 {
		return "", errNoHTTPBlock
	}

	braceIdx := strings.Index(content[httpIdx:], "{")
	if braceIdx < 0 {
		return "", errNoHTTPBlock
	}
	startIdx := httpIdx + braceIdx + 1

	depth := 1
	i := startIdx
	for i < len(content) && depth > 0 {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
		}
		if depth > 0 {
			i++
		}
	}

	if depth != 0 {
		return "", errNoHTTPBlock
	}

	insertPos := i
	includeContent := RenderIncludeEntry(panelDir)
	insertText := "\n" + includeContent + "\n"

	result := content[:insertPos] + insertText + content[insertPos:]
	return result, nil
}

func findHTTPBlock(content string) int {
	lines := strings.Split(content, "\n")
	pos := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			pos += len(line) + 1
			continue
		}
		if strings.HasPrefix(trimmed, "http ") || strings.HasPrefix(trimmed, "http{") || trimmed == "http" {
			return pos
		}
		if strings.HasPrefix(trimmed, "http") && strings.Contains(trimmed, "{") {
			return pos
		}
		pos += len(line) + 1
	}
	return -1
}

// DefaultWebUserOptions 自动检测 web 用户时的候选列表（可通过 SetWebUserCandidates 覆盖）
var DefaultWebUserOptions = []string{"www-data", "nginx", "www", "nobody"}

// SetWebUserCandidates 设置自动检测 web 用户的候选列表
func SetWebUserOptions(candidates []string) {
	if len(candidates) > 0 {
		DefaultWebUserOptions = candidates
	}
}

func ParseWebUser(content string) (user, group string) {
	depth := 0
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		depth += strings.Count(trimmed, "{") - strings.Count(trimmed, "}")

		if depth > 0 {
			continue
		}

		if strings.HasPrefix(trimmed, "user ") {
			rest := strings.TrimPrefix(trimmed, "user ")
			rest = strings.TrimSuffix(rest, ";")
			rest = strings.TrimSpace(rest)

			fields := strings.Fields(rest)
			if len(fields) == 0 {
				continue
			}
			user = fields[0]
			if len(fields) > 1 {
				group = fields[1]
			} else {
				group = user
			}
			return user, group
		}
	}

	return "", ""
}

func EnsurePanelDirectories(panelDir string) error {
	dirs := []string{
		panelDir + "/conf.d",
		panelDir + "/sites-available",
		panelDir + "/sites-enabled",
		panelDir + "/rewrite",
		panelDir + "/ssl",
		panelDir + "/backups",
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

var errNoHTTPBlock = &httpBlockError{}

type httpBlockError struct{}

func (e *httpBlockError) Error() string { return "未找到 http 块" }
