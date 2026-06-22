package nginx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTemplates_FromDir(t *testing.T) {
	dir := t.TempDir()
	writeTestTemplates(t, dir)

	ts, err := LoadTemplates(dir)
	if err != nil {
		t.Fatalf("LoadTemplates 失败: %v", err)
	}
	if ts == nil {
		t.Fatal("LoadTemplates 不应返回 nil")
	}
	if ts.Site == nil {
		t.Error("Site 模板不应为 nil")
	}
	if ts.StaticLocation == nil {
		t.Error("StaticLocation 模板不应为 nil")
	}
}

func TestLoadTemplates_MissingDir(t *testing.T) {
	_, err := LoadTemplates("/nonexistent/path")
	if err == nil {
		t.Error("不存在的目录应返回错误")
	}
}

func TestLoadTemplates_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadTemplates(dir)
	if err == nil {
		t.Error("缺少模板文件时应返回错误")
	}
}

func TestLoadTemplates_InvalidTemplate(t *testing.T) {
	dir := t.TempDir()

	for _, name := range requiredTemplateFiles() {
		content := "valid content"
		if name == "site.conf.tpl" {
			content = "{{.BadField.Undefined" // 无效模板语法
		}
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}

	_, err := LoadTemplates(dir)
	if err == nil {
		t.Error("无效模板应返回错误")
	}
}

func TestInitTemplates_MissingDir(t *testing.T) {
	err := InitTemplates("/nonexistent/path")
	if err == nil {
		t.Error("不存在的目录应返回 error")
	}
}

func TestInitTemplates_FromValidDir(t *testing.T) {
	dir := t.TempDir()
	writeTestTemplates(t, dir)

	storeMu.Lock()
	oldStore := store
	storeMu.Unlock()
	defer func() {
		storeMu.Lock()
		store = oldStore
		storeMu.Unlock()
	}()

	err := InitTemplates(dir)
	if err != nil {
		t.Fatalf("InitTemplates 失败: %v", err)
	}

	ts := GetTemplateStore()
	if ts == nil {
		t.Fatal("GetTemplateStore 不应返回 nil")
	}
	if ts.Site == nil {
		t.Error("Site 模板不应为 nil")
	}
}

func TestGetTemplateStore_NilWhenUninitialized(t *testing.T) {
	storeMu.Lock()
	oldStore := store
	store = nil
	storeMu.Unlock()
	defer func() {
		storeMu.Lock()
		store = oldStore
		storeMu.Unlock()
	}()

	ts := GetTemplateStore()
	if ts != nil {
		t.Error("未初始化时 GetTemplateStore 应返回 nil")
	}
}

func requiredTemplateFiles() []string {
	return []string{
		"site.conf.tpl", "ssl.conf.tpl", "force-https.conf.tpl",
		"location-static.conf.tpl",
		"location-proxy.conf.tpl", "include-entry.conf.tpl", "migrate-site.conf.tpl",
		"proxy-cache.conf.tpl",
	}
}

func writeTestTemplates(t *testing.T, dir string) {
	t.Helper()

	files := map[string]string{
		"site.conf.tpl": `#NXPANEL-SITE-START site_id={{.SiteID}}
server {
    listen 80;
}
#NXPANEL-SITE-END
`,
		"ssl.conf.tpl":             `    ssl_certificate {{.CertPath}};`,
		"force-https.conf.tpl":     `    return 301 https://$host$request_uri;`,
		"location-static.conf.tpl": `    location / { try_files $uri $uri/ =404; }`,
		"location-proxy.conf.tpl":  `    location / { proxy_pass {{.UpstreamURL}}; }`,
		"include-entry.conf.tpl":   `include {{.ConfDDir}}/*.conf;`,
		"migrate-site.conf.tpl":    `#NXPANEL-SITE-START site_id={{.SiteID}}`,
		"proxy-cache.conf.tpl":     `#NXPANEL-PROXY-CACHE-START\nproxy_cache_path {{.CachePath}} levels=1:2 keys_zone=proxy_cache_zone:10m max_size=100m inactive=60m;\n#NXPANEL-PROXY-CACHE-END`,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatalf("写入模板文件 %s 失败: %v", name, err)
		}
	}
}
