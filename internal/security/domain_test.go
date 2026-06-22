// security 包测试 — 域名验证测试
package security

import (
	"testing"
)

func TestValidateDomain(t *testing.T) {
	tests := []struct {
		name    string
		domain  string
		wantErr bool
	}{
		{"正常域名", "example.com", false},
		{"带 www", "www.example.com", false},
		{"多级子域名", "api.v1.example.com", false},
		{"短域名", "a.co", false},
		{"连字符", "my-site.example.com", false},
		{"通配符", "*.example.com", false},
		{"空域名", "", true},
		{"单标签", "localhost", true},
		{"以点开头", ".example.com", true},
		{"以点结尾", "example.com.", true},
		{"标签以连字符开头", "-bad.example.com", true},
		{"标签以连字符结尾", "bad-.example.com", true},
		{"非法字符下划线", "my_site.example.com", true},
		{"非法字符空格", "my site.example.com", true},
		{"纯通配符", "*.", true},
		{"超长标签", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDomain(tt.domain)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDomain(%q) error = %v, wantErr %v", tt.domain, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDomainList(t *testing.T) {
	t.Run("正常列表", func(t *testing.T) {
		result, err := ValidateDomainList([]string{"example.com", "www.example.com"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 domains, got %d", len(result))
		}
	})

	t.Run("去重", func(t *testing.T) {
		result, err := ValidateDomainList([]string{"example.com", "Example.Com", "EXAMPLE.COM"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 1 {
			t.Fatalf("expected 1 unique domain, got %d", len(result))
		}
	})

	t.Run("空列表", func(t *testing.T) {
		_, err := ValidateDomainList([]string{})
		if err == nil {
			t.Fatal("expected error for empty list")
		}
	})

	t.Run("包含非法域名", func(t *testing.T) {
		_, err := ValidateDomainList([]string{"example.com", "bad_domain.com"})
		if err == nil {
			t.Fatal("expected error for invalid domain")
		}
	})
}
