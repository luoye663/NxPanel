package repo

import "testing"

func TestGeoAccessRepoMultipleOrderedRules(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO sites
		(id, primary_domain, domains_json, status, root_path, access_log_path, error_log_path,
		 config_path, enabled_path, rewrite_path)
		VALUES ('site_geo', 'geo.example.com', '["geo.example.com"]', 'enabled', '/tmp',
		'/tmp/access.log', '/tmp/error.log', '/tmp/site.conf', '/tmp/site.enabled', '/tmp/site.rewrite')`); err != nil {
		t.Fatal(err)
	}
	store := NewGeoAccessRepo(database)
	settings, err := store.GetSiteSettings("site_geo")
	if err != nil || settings.Enabled || settings.DefaultAction != "allow" ||
		settings.DefaultStatusCode != 403 || settings.DefaultResponseType != "text" {
		t.Fatalf("unexpected defaults: %#v err=%v", settings, err)
	}
	settings.Enabled = true
	settings.DefaultAction = "respond"
	settings.DefaultStatusCode = 451
	settings.DefaultResponseType = "html"
	settings.DefaultResponseBody = "<h1>Unavailable</h1>"
	settings.ApplyStatus = "pending"
	if err := store.SaveSiteSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings.Enabled = false
	if err := store.SaveSiteSettings(settings); err != nil {
		t.Fatal(err)
	}
	pending, err := store.ListPendingDisabledSiteIDs()
	if err != nil || len(pending) != 1 || pending[0] != "site_geo" {
		t.Fatalf("unexpected pending disabled sites: %#v err=%v", pending, err)
	}
	settings.Enabled = true
	if err := store.SaveSiteSettings(settings); err != nil {
		t.Fatal(err)
	}
	for _, rule := range []*SiteGeoRule{
		{ID: "geo_1", SiteID: "site_geo", Name: "first", CountriesJSON: `["CN"]`, Action: "allow", Enabled: true, SortOrder: 0},
		{ID: "geo_2", SiteID: "site_geo", Name: "second", CountriesJSON: `["US"]`, Action: "deny_444", Enabled: true, SortOrder: 1},
	} {
		if err := store.CreateRule(rule); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ReorderRules("site_geo", []string{"geo_2", "geo_1"}); err != nil {
		t.Fatal(err)
	}
	rules, err := store.ListRules("site_geo")
	if err != nil || len(rules) != 2 || rules[0].ID != "geo_2" || rules[1].ID != "geo_1" {
		t.Fatalf("unexpected ordered rules: %#v err=%v", rules, err)
	}
	if err := store.ReorderRules("site_geo", []string{"geo_1"}); err == nil {
		t.Fatal("partial reorder must be rejected")
	}
}
