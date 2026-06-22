// rewrite 包测试 — 模板 CRUD 与校验
package rewrite

import (
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func boolPtr(v bool) *bool       { return &v }
func intPtr(v int) *int          { return &v }
func validInput() *TemplateInput {
	return &TemplateInput{
		Name:     "我的模板",
		Category: "custom",
		Template: "location / { try_files $uri /index.html; }",
		Params: []repo.RewriteTemplateParam{
			{Key: "upstream_url", Label: "地址", Type: "string", Default: "http://127.0.0.1:8080", Required: true},
		},
		Enabled:   boolPtr(true),
		SortOrder: intPtr(2),
	}
}

func TestCreateTemplate_Success(t *testing.T) {
	store := &mockTemplateStore{}
	svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, store)

	tpl, err := svc.CreateTemplate(validInput())
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if tpl.ID == "" {
		t.Error("应生成 ID")
	}
	if !strings.HasPrefix(tpl.ID, "rwtpl_") {
		t.Errorf("ID 前缀应为 rwtpl_，实际 %s", tpl.ID)
	}
	if !tpl.Enabled || tpl.SortOrder != 2 {
		t.Errorf("字段未正确写入: %+v", tpl)
	}
	if store.created == nil {
		t.Error("应调用 store.Create")
	}
}

func TestCreateTemplate_ValidationFailures(t *testing.T) {
	cases := []struct {
		name  string
		mutate func(*TemplateInput)
	}{
		{"空名称", func(i *TemplateInput) { i.Name = "  " }},
		{"名称过长", func(i *TemplateInput) { i.Name = strings.Repeat("a", 65) }},
		{"空模板体", func(i *TemplateInput) { i.Template = "   " }},
		{"模板体含空字节", func(i *TemplateInput) { i.Template = "a\x00b" }},
		{"模板体过大", func(i *TemplateInput) { i.Template = strings.Repeat("a", maxTemplateSize+1) }},
		{"参数 key 非法", func(i *TemplateInput) {
			i.Params = []repo.RewriteTemplateParam{{Key: "1bad", Label: "x", Type: "string"}}
		}},
		{"参数 key 含特殊字符", func(i *TemplateInput) {
			i.Params = []repo.RewriteTemplateParam{{Key: "a-b", Label: "x", Type: "string"}}
		}},
		{"参数 label 为空", func(i *TemplateInput) {
			i.Params = []repo.RewriteTemplateParam{{Key: "ok", Label: "  ", Type: "string"}}
		}},
		{"参数 type 非法", func(i *TemplateInput) {
			i.Params = []repo.RewriteTemplateParam{{Key: "ok", Label: "x", Type: "bogus"}}
		}},
		{"select 无 options", func(i *TemplateInput) {
			i.Params = []repo.RewriteTemplateParam{{Key: "ok", Label: "x", Type: "select", Options: nil}}
		}},
		{"参数 key 重复", func(i *TemplateInput) {
			i.Params = []repo.RewriteTemplateParam{
				{Key: "dup", Label: "x", Type: "string"},
				{Key: "dup", Label: "y", Type: "string"},
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, &mockTemplateStore{})
			input := validInput()
			c.mutate(input)
			_, err := svc.CreateTemplate(input)
			if !isAppErr(err, app.ErrValidationFailed) {
				t.Errorf("期望 VALIDATION_FAILED，实际: %v", err)
			}
		})
	}
}

func TestUpdateTemplate_NotFound(t *testing.T) {
	svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, &mockTemplateStore{})
	_, err := svc.UpdateTemplate("missing", validInput())
	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND，实际: %v", err)
	}
}

func TestUpdateTemplate_PreservesEnabledWhenOmitted(t *testing.T) {
	store := &mockTemplateStore{
		byID: map[string]*repo.RewriteTemplate{
			"t1": {ID: "t1", Name: "旧", Template: "x", Enabled: true, SortOrder: 7},
		},
	}
	svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, store)
	input := validInput()
	input.Enabled = nil // 不传 enabled
	input.SortOrder = nil
	tpl, err := svc.UpdateTemplate("t1", input)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if !tpl.Enabled {
		t.Error("省略 enabled 应保留原值 true")
	}
	if tpl.SortOrder != 7 {
		t.Errorf("省略 sort_order 应保留原值 7，实际 %d", tpl.SortOrder)
	}
}

func TestDeleteTemplate_Success(t *testing.T) {
	store := &mockTemplateStore{}
	svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, store)
	if err := svc.DeleteTemplate("rwtpl_x"); err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if store.deletedID != "rwtpl_x" {
		t.Errorf("应删除 rwtpl_x，实际 %s", store.deletedID)
	}
}

func TestListTemplates_Empty(t *testing.T) {
	svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, &mockTemplateStore{})
	resp, err := svc.ListTemplates()
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if len(resp.Templates) != 0 {
		t.Errorf("空存储应返回空切片，实际 %d", len(resp.Templates))
	}
}

func TestPreviewTemplate_LoadsFromStore(t *testing.T) {
	store := &mockTemplateStore{
		byID: map[string]*repo.RewriteTemplate{
			"spa": {ID: "spa", Template: "location / {\n    try_files $uri $uri/ /index.html;\n}\n"},
		},
	}
	svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, store)
	resp, err := svc.PreviewTemplate(&TemplatePreviewRequest{TemplateID: "spa"})
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if !strings.Contains(resp.Content, "try_files") {
		t.Errorf("预览应包含渲染内容，实际 %s", resp.Content)
	}
}

func TestPreviewTemplate_NotFound(t *testing.T) {
	svc := NewService(&mockSiteGetter{}, &mockRewriteMeta{}, &mockOpRecorder{}, &mockAgentTx{}, &mockTemplateStore{})
	_, err := svc.PreviewTemplate(&TemplatePreviewRequest{TemplateID: "missing"})
	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND，实际: %v", err)
	}
}

// TestRenderSSESeedTemplate 锁定 0023 种子模板（docker-proxy-sse）的 text/template 语法
// 能正确解析与渲染，避免迁移种子模板体存在语法错误时静默通过。
func TestRenderSSESeedTemplate(t *testing.T) {
	const sseBody = `location / {
    proxy_pass {{ .upstream_url }};
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header Connection "";
{{- if .pass_real_ip }}
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
{{- end }}
{{- if .websocket }}
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
{{- end }}
    # SSE：关闭缓冲并加长读超时，否则事件流会被攒批或超时断开
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout {{ .read_timeout }}s;
}
`
	tpl := repo.RewriteTemplate{
		ID:       "docker-proxy-sse",
		Template: sseBody,
		Params: []repo.RewriteTemplateParam{
			{Key: "upstream_url", Label: "目标地址", Type: "string", Default: "http://127.0.0.1:8080", Required: true},
			{Key: "pass_real_ip", Label: "传递真实 IP", Type: "boolean", Default: true},
			{Key: "websocket", Label: "WebSocket", Type: "boolean", Default: false},
			{Key: "read_timeout", Label: "读超时(秒)", Type: "number", Default: 3600, Required: true},
		},
	}

	// 全默认参数渲染
	out, err := renderTemplateContent(tpl, nil)
	if err != nil {
		t.Fatalf("默认渲染失败: %v", err)
	}
	if !strings.Contains(out, "proxy_read_timeout 3600s;") {
		t.Errorf("默认渲染应输出 proxy_read_timeout 3600s;，实际:\n%s", out)
	}
	if !strings.Contains(out, "proxy_buffering off;") {
		t.Errorf("渲染应包含 proxy_buffering off;，实际:\n%s", out)
	}
	// pass_real_ip 默认 true，应包含真实 IP 段
	if !strings.Contains(out, "X-Real-IP") {
		t.Errorf("pass_real_ip 默认开，应渲染 X-Real-IP，实际:\n%s", out)
	}
	// websocket 默认 false，不应包含 Upgrade
	if strings.Contains(out, "Upgrade") {
		t.Errorf("websocket 默认关，不应渲染 Upgrade，实际:\n%s", out)
	}

	// 自定义 read_timeout + websocket 渲染
	out2, err := renderTemplateContent(tpl, map[string]any{"read_timeout": 86400, "websocket": true})
	if err != nil {
		t.Fatalf("自定义渲染失败: %v", err)
	}
	if !strings.Contains(out2, "proxy_read_timeout 86400s;") {
		t.Errorf("应输出 proxy_read_timeout 86400s;，实际:\n%s", out2)
	}
	if !strings.Contains(out2, "Connection \"upgrade\";") {
		t.Errorf("websocket 开启应渲染 Connection upgrade，实际:\n%s", out2)
	}
}
