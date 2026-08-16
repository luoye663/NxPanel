package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestBrandingGetIsPublicAndUsesDefaults(t *testing.T) {
	server := newTestServer(t)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, apiTestPath(server, "/settings/branding"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"site_name":"NxPanel"`) || !strings.Contains(rec.Body.String(), `"subtitle":"开源 Nginx 网站管理面板"`) {
		t.Fatalf("unexpected defaults: %s", rec.Body.String())
	}
}

func TestBrandingGetFallsBackFromInvalidStoredValue(t *testing.T) {
	server := newTestServer(t)
	if err := repo.NewSettingsRepo(server.db).Set("branding", `{invalid`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, apiTestPath(server, "/settings/branding"), nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"site_name":"NxPanel"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBrandingUpdateRequiresAuthAndCSRF(t *testing.T) {
	server := newTestServer(t)
	body := `{"site_name":"  我的面板  ","subtitle":""}`

	unauthenticated := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, apiTestPath(server, "/settings/branding"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(unauthenticated, req)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status=%d body=%s", unauthenticated.Code, unauthenticated.Body.String())
	}

	setupTestAdmin(t, server)
	login := doLogin(server, "admin", "Test-password-123")
	csrfToken, sessionToken := parseLoginResponse(t, login)
	if sessionToken == "" || csrfToken == "" {
		t.Fatal("login did not return session and CSRF cookies")
	}

	withoutCSRF := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, apiTestPath(server, "/settings/branding"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionToken})
	server.Handler().ServeHTTP(withoutCSRF, req)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", withoutCSRF.Code, withoutCSRF.Body.String())
	}

	withCSRF := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, apiTestPath(server, "/settings/branding"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionToken})
	server.Handler().ServeHTTP(withCSRF, req)
	if withCSRF.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", withCSRF.Code, withCSRF.Body.String())
	}

	var response struct {
		Data struct {
			SiteName string `json:"site_name"`
			Subtitle string `json:"subtitle"`
		} `json:"data"`
	}
	if err := json.Unmarshal(withCSRF.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.SiteName != "我的面板" || response.Data.Subtitle != "" {
		t.Fatalf("unexpected response: %+v", response.Data)
	}

	persisted := httptest.NewRecorder()
	server.Handler().ServeHTTP(persisted, httptest.NewRequest(http.MethodGet, apiTestPath(server, "/settings/branding"), nil))
	if persisted.Code != http.StatusOK || !strings.Contains(persisted.Body.String(), `"site_name":"我的面板"`) {
		t.Fatalf("persisted status=%d body=%s", persisted.Code, persisted.Body.String())
	}
	operations, total, err := repo.NewOperationRepo(server.db).List(1, 10, "settings", "branding")
	if err != nil || total != 1 || operations[0].Status != "success" || operations[0].FinishedAt == nil {
		t.Fatalf("unexpected operation audit: total=%d operations=%+v err=%v", total, operations, err)
	}
}

func TestBrandingUpdateValidation(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	login := doLogin(server, "admin", "Test-password-123")
	csrfToken, sessionToken := parseLoginResponse(t, login)
	if sessionToken == "" || csrfToken == "" {
		t.Fatal("login did not return session and CSRF cookies")
	}

	tests := []string{
		`{"site_name":"   ","subtitle":"ok"}`,
		`{"site_name":"bad\nname","subtitle":"ok"}`,
		`{"site_name":"` + strings.Repeat("名", 81) + `","subtitle":"ok"}`,
		`{"site_name":"ok","subtitle":"` + strings.Repeat("副", 161) + `"}`,
		`{"site_name":"ok","subtitle":"","extra":true}`,
	}
	for _, body := range tests {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, apiTestPath(server, "/settings/branding"), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionToken})
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnprocessableEntity && rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
}
