package sites

import (
	"testing"
)

func TestValidateCreate(t *testing.T) {
	t.Run("正常请求", func(t *testing.T) {
		req := &CreateSiteRequest{
			Bindings: []Binding{{Domain: "example.com", Port: 80}},
			RootPath: "/www/wwwroot/example.com",
		}
		if err := ValidateCreate(req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("缺少绑定", func(t *testing.T) {
		req := &CreateSiteRequest{}
		if err := ValidateCreate(req); err == nil {
			t.Fatal("expected error for missing bindings")
		}
	})

	t.Run("非法域名", func(t *testing.T) {
		req := &CreateSiteRequest{
			Bindings: []Binding{{Domain: "bad_domain.com", Port: 80}},
		}
		if err := ValidateCreate(req); err == nil {
			t.Fatal("expected error for invalid domain")
		}
	})

	t.Run("端口不合法", func(t *testing.T) {
		req := &CreateSiteRequest{
			Bindings: []Binding{{Domain: "example.com", Port: 99999}},
			RootPath: "/www/wwwroot/example.com",
		}
		if err := ValidateCreate(req); err == nil {
			t.Fatal("expected error for invalid port")
		}
	})

	t.Run("根目录为空应报错", func(t *testing.T) {
		req := &CreateSiteRequest{
			Bindings: []Binding{{Domain: "example.com"}},
		}
		if err := ValidateCreate(req); err == nil {
			t.Fatal("expected error for empty root path")
		}
	})

	t.Run("默认值填充（仅 IndexFiles）", func(t *testing.T) {
		req := &CreateSiteRequest{
			Bindings: []Binding{{Domain: "example.com"}},
			RootPath: "/www/wwwroot/example.com",
		}
		if err := ValidateCreate(req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.IndexFiles != "index.html index.htm" {
			t.Errorf("expected default index files, got %s", req.IndexFiles)
		}
	})

	t.Run("根目录不在白名单", func(t *testing.T) {
		req := &CreateSiteRequest{
			Bindings: []Binding{{Domain: "example.com", Port: 80}},
			RootPath: "/etc/nginx/html",
		}
		if err := ValidateCreate(req); err == nil {
			t.Fatal("expected error for disallowed root path")
		}
	})

}

func TestValidateUpdate(t *testing.T) {
	t.Run("正常更新", func(t *testing.T) {
		req := &UpdateSiteRequest{
			HTTPSPort: 443,
			RootPath:  "/www/wwwroot/example.com",
		}
		if err := ValidateUpdate(req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("空请求通过", func(t *testing.T) {
		req := &UpdateSiteRequest{}
		if err := ValidateUpdate(req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("非法域名绑定", func(t *testing.T) {
		req := &UpdateSiteRequest{
			Bindings: []Binding{{Domain: "bad_domain.com", Port: 80}},
		}
		if err := ValidateUpdate(req); err == nil {
			t.Fatal("expected error for invalid domain")
		}
	})
}

func TestValidateDocumentAutoindexFormat(t *testing.T) {
	req := &UpdateSiteDocumentRequest{
		IndexFiles:      []string{"index.html"},
		AutoindexFormat: "json",
	}
	if _, err := ValidateDocument(req); err != nil {
		t.Fatalf("expected valid autoindex format: %v", err)
	}

	req.AutoindexFormat = "yaml"
	if _, err := ValidateDocument(req); err == nil {
		t.Fatal("expected invalid autoindex format error")
	}
}

func TestValidateDocumentAutoindexDefaultFormat(t *testing.T) {
	req := &UpdateSiteDocumentRequest{IndexFiles: []string{"index.html"}}
	if _, err := ValidateDocument(req); err != nil {
		t.Fatalf("expected default format: %v", err)
	}
	if req.AutoindexFormat != "html" {
		t.Fatalf("expected default html format, got %q", req.AutoindexFormat)
	}
}
