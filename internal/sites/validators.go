package sites

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/security"
)

var indexFileRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var autoindexFormats = map[string]bool{
	"html":  true,
	"xml":   true,
	"json":  true,
	"jsonp": true,
}

func ValidateCreate(req *CreateSiteRequest) error {
	if len(req.Bindings) == 0 {
		return fmt.Errorf("至少需要一条域名绑定")
	}

	if err := validateBindings(req.Bindings); err != nil {
		return err
	}

	if req.RootPath == "" {
		return fmt.Errorf("网站根目录不能为空")
	}
	if err := nginx.ValidateRootPath(req.RootPath); err != nil {
		return err
	}

	if req.IndexFiles == "" {
		req.IndexFiles = "index.html index.htm"
	}
	if err := nginx.ValidateIndexFiles(req.IndexFiles); err != nil {
		return err
	}

	return nil
}

func ValidateDocument(req *UpdateSiteDocumentRequest) (string, error) {
	if len(req.IndexFiles) == 0 {
		return "", fmt.Errorf("至少需要一个默认首页文件")
	}
	if len(req.IndexFiles) > 16 {
		return "", fmt.Errorf("默认首页最多 16 个")
	}

	cleaned := make([]string, 0, len(req.IndexFiles))
	for i, item := range req.IndexFiles {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		// 默认首页只允许站点根目录下的相对文件名，避免把 index 指令变成路径注入入口。
		if strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") || !indexFileRE.MatchString(name) {
			return "", fmt.Errorf("第 %d 个默认首页文件名不合法", i+1)
		}
		cleaned = append(cleaned, name)
	}
	joined := strings.Join(cleaned, " ")
	if joined == "" || len(joined) > 512 {
		return "", fmt.Errorf("默认首页不能为空且总长度不能超过 512 字节")
	}
	if err := nginx.ValidateIndexFiles(joined); err != nil {
		return "", err
	}
	if err := validateErrorPageURI(req.ErrorPage404); err != nil {
		return "", fmt.Errorf("404 页面 URI 不合法: %w", err)
	}
	if err := validateErrorPageURI(req.ErrorPage403); err != nil {
		return "", fmt.Errorf("403 页面 URI 不合法: %w", err)
	}
	req.AutoindexFormat = strings.TrimSpace(req.AutoindexFormat)
	if req.AutoindexFormat == "" {
		req.AutoindexFormat = "html"
	}
	if !autoindexFormats[req.AutoindexFormat] {
		return "", fmt.Errorf("目录浏览输出格式只允许 html、xml、json、jsonp")
	}
	return joined, nil
}

func validateErrorPageURI(uri string) error {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil
	}
	if !strings.HasPrefix(uri, "/") || strings.HasPrefix(uri, "//") || strings.Contains(uri, "..") || strings.ContainsAny(uri, ";{}\"\n\r\x00") || strings.Contains(uri, `\`) {
		return fmt.Errorf("必须是站点内 URI，例如 /404.html")
	}
	return nil
}

func ValidateUpdate(req *UpdateSiteRequest) error {
	if len(req.Bindings) > 0 {
		if err := validateBindings(req.Bindings); err != nil {
			return err
		}
	}

	if req.HTTPSPort != 0 {
		if err := nginx.ValidatePort(req.HTTPSPort); err != nil {
			return err
		}
	}

	if req.RootPath != "" {
		if err := nginx.ValidateRootPath(req.RootPath); err != nil {
			return err
		}
	}

	if req.IndexFiles != "" {
		if err := nginx.ValidateIndexFiles(req.IndexFiles); err != nil {
			return err
		}
	}

	return nil
}

func validateBindings(bindings []Binding) error {
	seen := make(map[string]bool)
	for i, b := range bindings {
		if b.Domain == "" {
			return fmt.Errorf("第 %d 条绑定域名为空", i+1)
		}
		if err := security.ValidateDomain(b.Domain); err != nil {
			return fmt.Errorf("第 %d 条绑定域名不合法: %w", i+1, err)
		}
		if b.Port == 0 {
			bindings[i].Port = 80
		}
		if err := nginx.ValidatePort(bindings[i].Port); err != nil {
			return fmt.Errorf("第 %d 条绑定端口不合法: %w", i+1, err)
		}
		if seen[b.Domain] {
			return fmt.Errorf("重复域名: %s", b.Domain)
		}
		seen[b.Domain] = true
	}
	return nil
}
