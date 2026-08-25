package geoaccess

import (
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestCompileCountryActionsFirstEnabledRuleWins(t *testing.T) {
	rules := []*repo.SiteGeoRule{
		{CountriesJSON: `["CN","US"]`, Action: ActionDeny403, Enabled: true},
		{CountriesJSON: `["CN","JP"]`, Action: ActionAllow, Enabled: true},
		{CountriesJSON: `["DE"]`, Action: ActionDeny444, Enabled: false},
	}
	got := compileCountryActions(rules)
	if got["CN"] != ActionDeny403 || got["US"] != ActionDeny403 || got["JP"] != ActionAllow {
		t.Fatalf("unexpected compiled actions: %#v", got)
	}
	if _, exists := got["DE"]; exists {
		t.Fatal("disabled rule must not contribute an action")
	}
}

func TestRenderConfigurationPriorityAndTrustedProxy(t *testing.T) {
	settings := []*repo.SiteGeoSettings{{SiteID: "site-1", Enabled: true, DefaultAction: ActionAllow}}
	rules := map[string][]*repo.SiteGeoRule{"site-1": {{CountriesJSON: `["CN"]`, Action: ActionDeny444, Enabled: true}}}
	ipRules := map[string][]*repo.SiteIPWhitelistRule{"site-1": {
		{RuleType: "allow", IPsJSON: `["10.0.0.9","10.0.0.0/24"]`, Enabled: true},
		{RuleType: "deny", IPsJSON: `["192.0.2.0/24"]`, Enabled: true},
	}}
	cache := &countryCache{BuildEpoch: 123, Networks: map[string][]string{"CN": {"1.0.1.0/24"}, "US": {"8.8.8.0/24"}}}

	rendered, err := renderConfiguration("/panel", []string{"10.0.0.0/8"}, cache, settings, rules, ipRules)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set_real_ip_from 10.0.0.0/8;",
		"real_ip_header X-Forwarded-For;",
		"1.0.1.0/24 deny_444;",
		"~^[01]:1: deny_403;",
		"~^[01]:0:1: allow;",
		"~^1:0:0: allow;",
	} {
		if !strings.Contains(rendered.GlobalContent, want) {
			t.Errorf("global config missing %q:\n%s", want, rendered.GlobalContent)
		}
	}
	site := rendered.Sites["site-1"]
	if !strings.Contains(site.AllowBody, "10.0.0.0/24 1;") || !strings.Contains(site.DenyBody, "192.0.2.0/24 1;") {
		t.Fatalf("IP exception files not rendered: allow=%q deny=%q", site.AllowBody, site.DenyBody)
	}
}

func TestPatchAccessLimitIsIdempotentAndRemovable(t *testing.T) {
	original := []byte("# existing\nauth_basic off;\n")
	first := patchAccessLimit(original, "/panel/geo-access/sites/site-1.conf", true)
	second := patchAccessLimit(first, "/panel/geo-access/sites/site-1.conf", true)
	if string(first) != string(second) {
		t.Fatalf("patch must be idempotent:\nfirst=%q\nsecond=%q", first, second)
	}
	if strings.Count(string(second), geoIncludeStart) != 1 {
		t.Fatalf("expected one managed marker: %q", second)
	}
	removed := patchAccessLimit(second, "", false)
	if strings.Contains(string(removed), "geo-access") || !strings.Contains(string(removed), "auth_basic off;") {
		t.Fatalf("disable must remove only the managed block: %q", removed)
	}
}

func TestRenderConfigurationDoesNothingWithoutEnabledSites(t *testing.T) {
	rendered, err := renderConfiguration("/panel", nil, &countryCache{}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rendered.GlobalContent != "" || len(rendered.Sites) != 0 {
		t.Fatalf("disabled feature generated config: %#v", rendered)
	}
}
