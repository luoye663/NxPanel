package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCertificateDiscoveryAuthenticationAndCSRF(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	for _, test := range []struct{ method, path string }{
		{"GET", "/sites/site/certificates/scan"}, {"POST", "/certificates/adopt"},
		{"POST", "/sites/site/certificates/replace-preview"}, {"POST", "/sites/site/certificates/replace"},
	} {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(test.method, apiTestPath(server, test.path), nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s auth: %d %s", test.path, rec.Code, rec.Body.String())
		}
	}
	login := doLogin(server, "admin", "Test-password-123")
	csrf, cookie := parseLoginResponse(t, login)
	for _, test := range []struct {
		csrf, body string
		status     int
	}{
		{"", `{"site_id":"site","expected_hash":"hash"}`, http.StatusForbidden},
		{csrf, `{"site_id":"site","expected_hash":"hash","cert_path":"/arbitrary"}`, http.StatusBadRequest},
		{csrf, `{"site_id":"site","expected_hash":"hash"}{}`, http.StatusBadRequest},
	} {
		req := httptest.NewRequest("POST", apiTestPath(server, "/certificates/adopt"), strings.NewReader(test.body))
		req.AddCookie(&http.Cookie{Name: "openrest_session", Value: cookie})
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", test.csrf)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != test.status {
			t.Fatalf("expected %d, got %d: %s", test.status, rec.Code, rec.Body.String())
		}
	}
}
