package nginx

import "testing"

func TestValidateRootPathPrefixBoundary(t *testing.T) {
	SetAllowedRootPrefixes([]string{"/var/www"})
	t.Cleanup(func() {
		SetAllowedRootPrefixes([]string{"/www/wwwroot", "/var/www"})
	})

	if err := ValidateRootPath("/var/www/example.com"); err != nil {
		t.Fatalf("expected /var/www/example.com to be allowed: %v", err)
	}
	if err := ValidateRootPath("/var/www2/example.com"); err == nil {
		t.Fatal("expected /var/www2/example.com to be rejected")
	}
}

func TestValidateLogPathAllowedPrefixes(t *testing.T) {
	SetAllowedLogPrefixes([]string{"/var/log/nginx/nxpanel"})
	t.Cleanup(func() {
		SetAllowedLogPrefixes([]string{"/www/wwwlogs", "/var/log/nginx/nxpanel"})
	})

	if err := ValidateLogPath("/var/log/nginx/nxpanel/site.access.log"); err != nil {
		t.Fatalf("expected nxpanel log path to be allowed: %v", err)
	}
	if err := ValidateLogPath("/var/log/nginx/site.access.log"); err == nil {
		t.Fatal("expected broad nginx log path to be rejected")
	}
}
