package nginx

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"text/template"
)

type TemplateStore struct {
	Site           *template.Template
	SSL            *template.Template
	ForceHTTPS     *template.Template
	StaticLocation *template.Template
	ProxyLocation  *template.Template
	IncludeEntry   *template.Template
	MigrateSite    *template.Template
	ProxyCache     *template.Template
}

var (
	store   *TemplateStore
	storeMu sync.RWMutex
)

func GetTemplateStore() *TemplateStore {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return store
}

func InitTemplates(dir string) error {
	s, err := LoadTemplates(dir)
	if err != nil {
		return fmt.Errorf("加载 Nginx 模板失败: %w", err)
	}
	storeMu.Lock()
	store = s
	storeMu.Unlock()
	slog.Info("Nginx 模板加载成功", "dir", dir)
	return nil
}

func LoadTemplates(dir string) (*TemplateStore, error) {
	ts := &TemplateStore{}
	targets := map[string]**template.Template{
		"site.conf.tpl":            &ts.Site,
		"ssl.conf.tpl":             &ts.SSL,
		"force-https.conf.tpl":     &ts.ForceHTTPS,
		"location-static.conf.tpl": &ts.StaticLocation,
		"location-proxy.conf.tpl":  &ts.ProxyLocation,
		"include-entry.conf.tpl":   &ts.IncludeEntry,
		"migrate-site.conf.tpl":    &ts.MigrateSite,
		"proxy-cache.conf.tpl":     &ts.ProxyCache,
	}

	for name, target := range targets {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("读取模板 %s 失败: %w", name, err)
		}
		tmpl, err := template.New(name).Parse(string(data))
		if err != nil {
			return nil, fmt.Errorf("解析模板 %s 失败: %w", name, err)
		}
		*target = tmpl
	}

	return ts, nil
}
