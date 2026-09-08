package accesspolicy

import (
	"reflect"
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestLegacyDraftPreservesRulesAndDoesNotActivate(t *testing.T) {
	database := policyTestDB(t)
	ipRepo := repo.NewIPWhitelistRuleRepo(database)
	for _, rule := range []*repo.SiteIPWhitelistRule{
		{ID: "allow", SiteID: "site_policy", Name: "office", RuleType: "allow", IPsJSON: `["192.0.2.0/24"]`, Enabled: true},
		{ID: "deny", SiteID: "site_policy", Name: "blocked", RuleType: "deny", IPsJSON: `["192.0.2.3"]`, Enabled: true},
		{ID: "disabled", SiteID: "site_policy", Name: "disabled", RuleType: "allow", IPsJSON: `["203.0.113.1"]`, Enabled: false},
	} {
		if err := ipRepo.Create(rule); err != nil {
			t.Fatal(err)
		}
	}
	geoRepo := repo.NewGeoAccessRepo(database)
	settings, err := geoRepo.GetSiteSettings("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled, settings.DefaultAction = true, "respond"
	settings.DefaultStatusCode, settings.DefaultResponseType, settings.DefaultResponseBody = 451, "html", "<p>Unavailable</p>"
	if err := geoRepo.SaveSiteSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []*repo.SiteGeoRule{
		{ID: "geo_allow", SiteID: "site_policy", Name: "allow CN", CountriesJSON: `["CN"]`, Enabled: true, Action: "allow", SortOrder: 0},
		{ID: "geo_deny", SiteID: "site_policy", Name: "deny CN US", CountriesJSON: `["CN","US"]`, Enabled: true, Action: "deny_444", SortOrder: 1},
	} {
		if err := geoRepo.CreateRule(rule); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.NewDenyRuleRepo(database).Create(&repo.SiteDenyRule{ID: "deny_path", SiteID: "site_policy", Name: "private", Enabled: true,
		DenyType: "extension", ExtensionPattern: "bak,sql", PathPattern: "/private"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.NewHotlinkRuleRepo(database).Create(&repo.SiteHotlinkRule{ID: "hotlink", SiteID: "site_policy", Name: "images", Enabled: true,
		Extensions: "jpg,png", Referers: "server_names,*.example.org,blocked", AllowEmptyReferer: true, BlockStatus: 403}); err != nil {
		t.Fatal(err)
	}
	account := &repo.AuthAccount{ID: "account", Scope: "site", SiteID: "site_policy", Username: "user", PasswordHash: "{SHA}hash", Enabled: true}
	if err := repo.NewAuthAccountRepo(database).Create(account); err != nil {
		t.Fatal(err)
	}
	authRepo := repo.NewAuthRuleRepo(database)
	if err := authRepo.Create(&repo.SiteAuthRule{ID: "auth", SiteID: "site_policy", Name: "admin", Enabled: true, Path: "/admin"}); err != nil {
		t.Fatal(err)
	}
	if err := authRepo.SetAccountIDs("auth", []string{"account"}); err != nil {
		t.Fatal(err)
	}
	proxyRepo := repo.NewProxyRepo(database)
	if err := proxyRepo.Create(&repo.SiteProxy{ID: "proxy", SiteID: "site_policy", Name: "api", Enabled: false,
		LocationPath: "/api", UpstreamURL: "http://127.0.0.1:9000", AuthEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := proxyRepo.SetAccountIDs("proxy", []string{"account"}); err != nil {
		t.Fatal(err)
	}
	r := NewRepo(database)
	draft, err := r.LegacyDraft("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(*draft); err != nil {
		t.Fatalf("ordinary imported draft must validate: %v", err)
	}
	if draft.Mode != "legacy" || draft.Version != 0 || len(draft.Warnings) < 3 || len(draft.Rules) != 11 {
		t.Fatalf("unexpected draft: %#v", draft)
	}
	if persisted, err := r.Get("site_policy"); err != nil || persisted != nil {
		t.Fatalf("draft read persisted changes: %#v, %v", persisted, err)
	}
	bySource := map[string]Rule{}
	positions := map[string]int{}
	for i, rule := range draft.Rules {
		bySource[rule.SourceID] = rule
		positions[rule.SourceID] = i
	}
	if bySource["disabled"].Enabled || !bySource["proxy"].Enabled || !bySource["proxy"].SourceDisabled {
		t.Fatal("disabled legacy rules were enabled")
	}
	if !reflect.DeepEqual(bySource["auth"].Action.AccountIDs, []string{"account"}) || !reflect.DeepEqual(bySource["proxy"].Action.AccountIDs, []string{"account"}) {
		t.Fatal("auth account references lost")
	}
	if bySource["deny_path"].Match != "any" || len(bySource["deny_path"].Conditions) != 2 {
		t.Fatal("independent old suffix/path locations must be OR")
	}
	if bySource["unmatched"].Action.ResponseBody != settings.DefaultResponseBody || bySource["unmatched"].Action.StatusCode != 451 {
		t.Fatal("custom geo fallback response lost")
	}
	if len(bySource["geo_deny"].Conditions) != 3 || !bySource["geo_deny"].Conditions[1].Negate || !reflect.DeepEqual(bySource["geo_deny"].Conditions[1].Values, []string{"CN"}) {
		t.Fatal("earlier geo allow must remain exempt from later deny")
	}
	if !bySource["whitelist_unmatched"].Conditions[0].Negate || !reflect.DeepEqual(bySource["whitelist_unmatched"].Conditions[0].Values, []string{"192.0.2.0/24"}) {
		t.Fatal("whitelist complement must use only enabled whitelist entries")
	}
	if positions["allow"] < positions["auth"] || positions["allow"] < positions["hotlink"] {
		t.Fatal("imported whitelist allow must not bypass existing restrictions")
	}
	wantRefs := []string{"policy.example.com", "*.example.org", "blocked", "none"}
	if !reflect.DeepEqual(bySource["hotlink"].Conditions[1].Values, wantRefs) {
		t.Fatalf("hotlink referers: %#v", bySource["hotlink"])
	}
	again, err := r.LegacyDraft("site_policy")
	if err != nil || !reflect.DeepEqual(draft, again) {
		t.Fatalf("draft must be deterministic: %v", err)
	}
}

func TestLegacyDraftDisabledGeoStaysDisabled(t *testing.T) {
	database := policyTestDB(t)
	if err := repo.NewGeoAccessRepo(database).CreateRule(&repo.SiteGeoRule{ID: "geo", SiteID: "site_policy", Name: "geo", CountriesJSON: `["CN"]`, Enabled: true, Action: "deny_403"}); err != nil {
		t.Fatal(err)
	}
	draft, err := NewRepo(database).LegacyDraft("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range draft.Rules {
		if rule.Enabled {
			t.Fatalf("global disabled geo activated: %#v", rule)
		}
	}
}

func TestLegacyDraftUnsupportedRegexRequiresExplicitCorrection(t *testing.T) {
	database := policyTestDB(t)
	if err := repo.NewDenyRuleRepo(database).Create(&repo.SiteDenyRule{ID: "regex", SiteID: "site_policy", Name: "regex", Enabled: true,
		DenyType: "extension", ExtensionPattern: "php[0-9]?"}); err != nil {
		t.Fatal(err)
	}
	draft, err := NewRepo(database).LegacyDraft("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	if len(draft.Rules) != 1 || !draft.Rules[0].Enabled || draft.Rules[0].Conditions[0].Values[0] != "php[0-9]?" || len(draft.Warnings) == 0 {
		t.Fatalf("unsupported restriction must remain visible: %#v", draft)
	}
	if err := Validate(*draft); err == nil {
		t.Fatal("unsupported restriction must block activation until edited")
	}
}

func TestLegacyDraftPropagatesCorruptData(t *testing.T) {
	database := policyTestDB(t)
	if _, err := database.Exec(`INSERT INTO site_geo_rules (id, site_id, name, countries_json, action) VALUES ('bad', 'site_policy', 'bad', 'not-json', 'allow')`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepo(database).LegacyDraft("site_policy"); err == nil {
		t.Fatal("malformed old data must not silently produce missing restrictions")
	}
}
