// repo 包 — rewrite_templates 表集成测试
package repo

import (
	"strings"
	"testing"
)

func TestRewriteTemplateRepo_SeedBuiltins(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	r := NewRewriteTemplateRepo(database)

	all, err := r.List()
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("迁移应种子至少 3 个内置模板，实际 %d", len(all))
	}

	spa, err := r.GetByID("spa")
	if err != nil {
		t.Fatalf("GetByID spa 失败: %v", err)
	}
	if spa == nil {
		t.Fatal("内置模板 spa 应存在")
	}
	if spa.Name == "" || spa.Template == "" {
		t.Errorf("spa 字段不完整: %+v", spa)
	}
	if !spa.Enabled {
		t.Error("内置模板 spa 应默认启用")
	}
	if len(spa.Params) != 0 {
		t.Errorf("spa 应无参数，实际 %d", len(spa.Params))
	}

	dp, err := r.GetByID("docker-proxy")
	if err != nil {
		t.Fatalf("GetByID docker-proxy 失败: %v", err)
	}
	if dp == nil {
		t.Fatal("内置模板 docker-proxy 应存在")
	}
	if len(dp.Params) != 3 {
		t.Errorf("docker-proxy 应有 3 个参数，实际 %d", len(dp.Params))
	}
	if dp.Params[0].Key != "upstream_url" || !dp.Params[0].Required {
		t.Errorf("docker-proxy 第一个参数不正确: %+v", dp.Params[0])
	}

	// 0023 种子：SSE 变体
	sse, err := r.GetByID("docker-proxy-sse")
	if err != nil {
		t.Fatalf("GetByID docker-proxy-sse 失败: %v", err)
	}
	if sse == nil {
		t.Fatal("内置模板 docker-proxy-sse 应存在")
	}
	if !strings.Contains(sse.Template, "proxy_buffering off") {
		t.Errorf("SSE 模板应包含 proxy_buffering off，实际 %q", sse.Template)
	}
	if !strings.Contains(sse.Template, "proxy_read_timeout") {
		t.Errorf("SSE 模板应包含 proxy_read_timeout，实际 %q", sse.Template)
	}
	var readTimeout *RewriteTemplateParam
	for _, p := range sse.Params {
		if p.Key == "read_timeout" {
			readTimeout = &p
		}
	}
	if readTimeout == nil {
		t.Fatal("SSE 模板应包含 read_timeout 参数")
	}
	if readTimeout.Type != "number" || !readTimeout.Required {
		t.Errorf("read_timeout 参数应为 number 且必填: %+v", readTimeout)
	}
}

func TestRewriteTemplateRepo_CRUD(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	r := NewRewriteTemplateRepo(database)

	tpl := &RewriteTemplate{
		ID:          "rwtpl_test_1",
		Name:        "测试模板",
		Category:    "custom",
		Description: "测试描述",
		Params: []RewriteTemplateParam{
			{Key: "port", Label: "端口", Type: "string", Default: "8080", Required: true},
			{Key: "tls", Label: "TLS", Type: "boolean", Default: false},
		},
		Template:  "location / { proxy_pass http://127.0.0.1:{{ .port }}; }",
		Enabled:   true,
		SortOrder: 5,
	}

	if err := r.Create(tpl); err != nil {
		t.Fatalf("Create 失败: %v", err)
	}
	if tpl.CreatedAt == "" || tpl.UpdatedAt == "" {
		t.Error("Create 后应回填时间戳")
	}

	got, err := r.GetByID("rwtpl_test_1")
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if got.Name != "测试模板" || len(got.Params) != 2 {
		t.Errorf("GetByID 字段不匹配: %+v", got)
	}
	if got.Params[1].Default != false {
		t.Errorf("boolean 默认值应正确往返，实际 %v", got.Params[1].Default)
	}

	// ListEnabled 应包含种子 + 新建
	enabled, err := r.ListEnabled()
	if err != nil {
		t.Fatalf("ListEnabled 失败: %v", err)
	}
	if len(enabled) < 3 {
		t.Errorf("ListEnabled 应至少 3 项，实际 %d", len(enabled))
	}

	// 更新
	got.Name = "改名"
	got.Enabled = false
	got.SortOrder = 9
	if err := r.Update(got); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
	updated, _ := r.GetByID("rwtpl_test_1")
	if updated.Name != "改名" || updated.Enabled || updated.SortOrder != 9 {
		t.Errorf("Update 后字段不正确: %+v", updated)
	}

	// 禁用后不应出现在 ListEnabled
	enabledAfter, _ := r.ListEnabled()
	for _, e := range enabledAfter {
		if e.ID == "rwtpl_test_1" {
			t.Error("禁用模板不应出现在 ListEnabled")
		}
	}

	// 删除
	if err := r.Delete("rwtpl_test_1"); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
	deleted, _ := r.GetByID("rwtpl_test_1")
	if deleted != nil {
		t.Error("删除后应查不到")
	}

	// 删除不存在的应返回 ErrNoRows
	if err := r.Delete("rwtpl_test_1"); err == nil {
		t.Error("删除不存在的模板应返回错误")
	}
}

func TestRewriteTemplateRepo_GetByID_NotFound(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	r := NewRewriteTemplateRepo(database)
	got, err := r.GetByID("nonexistent")
	if err != nil {
		t.Fatalf("未期望错误: %v", err)
	}
	if got != nil {
		t.Error("不存在的模板应返回 nil")
	}
}
