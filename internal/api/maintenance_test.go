package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestMaintenanceRoutesAuthCSRFAndWriteGuard(t *testing.T) {
	s := newTestServer(t)
	setupTestAdmin(t, s)
	for _, test := range []struct{ method, path string }{{"GET", "/sites/site/maintenance"}, {"PUT", "/sites/site/maintenance"}, {"POST", "/sites/site/maintenance/sync"}, {"GET", "/settings/maintenance"}, {"PUT", "/settings/maintenance"}} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(test.method, apiTestPath(s, test.path), nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("auth %s: %d %s", test.path, rec.Code, rec.Body.String())
		}
	}
	login := doLogin(s, "admin", "Test-password-123")
	csrf, cookie := parseLoginResponse(t, login)
	request := func(method, path, body, token string) int {
		t.Helper()
		req := httptest.NewRequest(method, apiTestPath(s, path), strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "openrest_session", Value: cookie})
		req.Header.Set("X-CSRF-Token", token)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}
	if code := request("PUT", "/sites/site/maintenance", `{}`, ""); code != 403 {
		t.Fatalf("CSRF: %d", code)
	}
	if code := request("PUT", "/sites/site/maintenance", `{"unknown":true}`, csrf); code != 400 {
		t.Fatalf("unknown fields: %d", code)
	}
	if code := request("GET", "/settings/maintenance", "", ""); code != 200 {
		t.Fatalf("defaults route: %d", code)
	}
	if err := repo.NewSiteRepo(s.db).Create(&repo.Site{ID: "site", PrimaryDomain: "example.com", DomainsJSON: `["example.com"]`, Status: "enabled", ConfigPath: "/config/site.conf", EnabledPath: "/config/site.conf", RootPath: "/www"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO site_maintenance(site_id,apply_status) VALUES('site','pending')`); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, path string }{{"PUT", "/sites/site"}, {"POST", "/sites/site/disable"}, {"DELETE", "/sites/site"}, {"PUT", "/sites/site/config"}, {"PUT", "/nginx/conf"}, {"POST", "/files/write"}} {
		if code := request(test.method, test.path, `{}`, csrf); code != 409 {
			t.Fatalf("pending guard %s %s: %d", test.method, test.path, code)
		}
	}
}
