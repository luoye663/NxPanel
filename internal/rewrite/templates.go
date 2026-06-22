// rewrite 包 — Location 模板渲染与参数归一化
//
// 模板数据存储在 rewrite_templates 表（见 0022 迁移），由用户在面板内动态管理。
// 本文件仅保留与具体存储无关的纯逻辑：参数归一化、模板渲染。
package rewrite

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/security"
)

// renderTemplateContent 用给定参数渲染模板内容，返回 nginx 片段字符串。
func renderTemplateContent(tpl repo.RewriteTemplate, rawParams map[string]any) (string, error) {
	params, err := normalizeTemplateParams(tpl, rawParams)
	if err != nil {
		return "", err
	}
	parsed, err := template.New(tpl.ID).Option("missingkey=error").Parse(tpl.Template)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, "解析 Location 模板失败: "+err.Error(), nil)
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, params); err != nil {
		return "", app.NewAppError(app.ErrValidationFailed, "渲染 Location 模板失败: "+err.Error(), nil)
	}
	return buf.String(), nil
}

func normalizeTemplateParams(tpl repo.RewriteTemplate, raw map[string]any) (map[string]any, error) {
	params := make(map[string]any, len(tpl.Params))
	for _, param := range tpl.Params {
		value, ok := raw[param.Key]
		if !ok || value == nil || value == "" {
			value = param.Default
		}
		if param.Required && value == nil {
			return nil, app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 不能为空", param.Label), nil)
		}
		switch param.Type {
		case "string":
			text := strings.TrimSpace(fmt.Sprint(value))
			// URL 参数复用现有 upstream 校验，避免模板参数注入 Nginx 指令。
			if strings.HasSuffix(param.Key, "_url") {
				if err := security.ValidateUpstreamURL(text); err != nil {
					return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
				}
			} else if strings.ContainsAny(text, ";{}\"\n\r\x00") {
				return nil, app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 包含非法字符", param.Label), nil)
			}
			params[param.Key] = text
		case "boolean":
			params[param.Key] = toBool(value)
		case "number":
			n, err := strconv.Atoi(fmt.Sprint(value))
			if err != nil {
				return nil, app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 必须是数字", param.Label), nil)
			}
			params[param.Key] = n
		case "select":
			text := fmt.Sprint(value)
			valid := false
			for _, option := range param.Options {
				valid = valid || option == text
			}
			if !valid {
				return nil, app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("参数 %s 选项非法", param.Label), nil)
			}
			params[param.Key] = text
		}
	}
	return params, nil
}

func toBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "on"
	default:
		return fmt.Sprint(value) == "true"
	}
}
