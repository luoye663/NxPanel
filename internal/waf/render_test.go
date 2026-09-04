package waf

import (
	"strings"
	"testing"
)

func validPolicy() SitePolicy {
	p := DefaultSitePolicy("site_1")
	p.Audit = AuditConfig{Enabled: true, StorageDir: "/opt/nxpanel/nginx/waf/audit/site_1"}
	p.ModSecurityConf = "/opt/nxpanel/nginx/waf/modsecurity.conf"
	p.CRSSetupPath = "/opt/nxpanel/nginx/waf/rules/versions/4.25.0/crs-setup.conf"
	p.CRSRulesGlob = "/opt/nxpanel/nginx/waf/rules/versions/4.25.0/rules/*.conf"
	p.Exclusions = []Exclusion{{RuleID: "942100", Param: "username"}, {RuleID: "930100"}}
	return p
}

func TestRenderSiteConfigCRS4AndAudit(t *testing.T) {
	p := validPolicy()
	p.Mode = ModeOn
	p.ParanoiaLevel = 3
	p.ResponseStatus = 406
	gotBytes, err := RenderSiteConfig(p, []string{"/opt/nxpanel/nginx/waf"})
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)
	for _, want := range []string{
		"SecRuleEngine On",
		"setvar:tx.blocking_paranoia_level=3",
		"SecAuditLogType Concurrent",
		"SecAuditLogParts ABCDEFGHIJKZ",
		"SecAuditLogDirMode 0750",
		"SecAuditLogFileMode 0640",
		`SecRuleUpdateActionById 949110 "status:406"`,
		`SecRule ARGS_NAMES "@streq username"`,
		"SecRuleRemoveById 930100",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered config does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "tx.paranoia_level") {
		t.Fatalf("legacy CRS3 paranoia variable was rendered:\n%s", got)
	}
	if strings.Index(got, "Include "+p.ModSecurityConf) > strings.Index(got, "SecRuleEngine On") {
		t.Fatal("baseline config must be included before site policy overrides")
	}
}

func TestRenderSiteConfigDisabled(t *testing.T) {
	p := validPolicy()
	p.Enabled = false
	got, err := RenderSiteConfig(p, []string{"/opt/nxpanel/nginx/waf"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Managed by nxPanel WAF provider; manual changes will be overwritten.\nmodsecurity off;\n" {
		t.Fatalf("unexpected disabled config: %s", got)
	}
}

func TestSitePolicyRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name string
		edit func(*SitePolicy)
	}{
		{"site traversal", func(p *SitePolicy) { p.SiteID = "../site" }},
		{"unknown mode", func(p *SitePolicy) { p.Mode = "on" }},
		{"bad paranoia", func(p *SitePolicy) { p.ParanoiaLevel = 5 }},
		{"outside path", func(p *SitePolicy) { p.CRSSetupPath = "/etc/passwd" }},
		{"path injection", func(p *SitePolicy) { p.ModSecurityConf = "/opt/nxpanel/nginx/waf/a.conf;include /etc/passwd" }},
		{"selector injection", func(p *SitePolicy) { p.Exclusions = []Exclusion{{RuleID: "942100", Path: "/\" bad"}} }},
		{"multiple selectors", func(p *SitePolicy) { p.Exclusions = []Exclusion{{RuleID: "942100", Path: "/x", Param: "x"}} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validPolicy()
			tt.edit(&p)
			if _, err := RenderSiteConfig(p, []string{"/opt/nxpanel/nginx/waf"}); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestRuntimeFingerprintIntegrity(t *testing.T) {
	f, err := NewRuntimeFingerprint("nginx", "nginx/1.27.0", strings.Repeat("a", 64), strings.Repeat("b", 64), "linux", "amd64", "musl")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	f.GOARCH = "arm64"
	if err := f.Validate(); err == nil {
		t.Fatal("tampered fingerprint must fail")
	}
}

func TestValidateProviderIsFixedAllowlist(t *testing.T) {
	if err := ValidateProvider(ProviderID, ABIVersion); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		id  string
		abi int
	}{{"other", ABIVersion}, {ProviderID, 2}} {
		if err := ValidateProvider(test.id, test.abi); err == nil {
			t.Fatalf("provider %q ABI %d should be rejected", test.id, test.abi)
		}
	}
}
