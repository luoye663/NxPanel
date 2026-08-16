package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
)

func newNoAgentTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &app.Config{
		LogLevel: "info",
		API:      app.APIConfig{Listen: "127.0.0.1:0", LoginPath: "/nx-testgate"},
		Nginx:    app.NginxConfig{ConfPath: "/etc/nginx/nginx.conf"},
	}
	database := newTestDB(t)
	server, err := NewServer(cfg, database)
	if err != nil {
		t.Fatalf("创建无 Agent 测试服务器失败: %v", err)
	}
	return server
}

func TestSetupAdmin_UnknownFieldRejected(t *testing.T) {
	server := newTestServerWithAgent(t)

	req := httptest.NewRequest("POST", apiTestPath(server, "/setup/admin"), strings.NewReader(`{"username":"admin","password":"Test-password-123","extra":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应返回 400，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestSetupAdmin_RequiresAgent(t *testing.T) {
	server := newNoAgentTestServer(t)

	req := httptest.NewRequest("POST", apiTestPath(server, "/setup/admin"), strings.NewReader(`{"username":"admin","password":"Test-password-123"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("Agent 未启动时初始化应返回 503，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestLogin_UnknownFieldRejected(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	req := httptest.NewRequest("POST", apiTestPath(server, "/auth/login"), strings.NewReader(`{"username":"admin","password":"Test-password-123","extra":true}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应返回 400，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestLogin_MultipleJSONValuesRejected(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	body := `{"username":"admin","password":"Test-password-123"}{"username":"admin","password":"Test-password-123"}`
	req := httptest.NewRequest("POST", apiTestPath(server, "/auth/login"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("多个 JSON 对象应返回 400，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestProxyCreateUnknownFieldRejected(t *testing.T) {
	server := newTestServerWithAgent(t)
	setupTestAdmin(t, server)
	body := `{"name":"proxy","location_path":"/","upstream_url":"http://127.0.0.1:8080","host_header":"$host","unexpected":true}`
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, authenticatedUpstreamRequest(t, server, http.MethodPost, "/sites/site_missing/proxy", body, true))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("proxy unknown field status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestProxySyncRequiresAuthAndCSRFAndUsesStaticRoute(t *testing.T) {
	server := newTestServerWithAgent(t)
	setupTestAdmin(t, server)
	unauthenticated := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodPost, apiTestPath(server, "/sites/site_missing/proxy/sync"), nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("proxy sync unauthenticated status=%d", unauthenticated.Code)
	}
	withoutCSRF := httptest.NewRecorder()
	server.Handler().ServeHTTP(withoutCSRF, authenticatedUpstreamRequest(t, server, http.MethodPost, "/sites/site_missing/proxy/sync", "", false))
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("proxy sync missing CSRF status=%d body=%s", withoutCSRF.Code, withoutCSRF.Body.String())
	}
	withCSRF := httptest.NewRecorder()
	server.Handler().ServeHTTP(withCSRF, authenticatedUpstreamRequest(t, server, http.MethodPost, "/sites/site_missing/proxy/sync", "", true))
	if withCSRF.Code != http.StatusNotFound {
		t.Fatalf("proxy sync static route status=%d body=%s", withCSRF.Code, withCSRF.Body.String())
	}
}

func TestHandleNginxReload_OptionalBody(t *testing.T) {
	server := newNoAgentTestServer(t)

	t.Run("empty body allowed", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/nginx/reload", strings.NewReader(""))
		req = req.WithContext(applyRequestID(req))
		rec := httptest.NewRecorder()

		server.handleNginxReload(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("空 body 应通过可选解码并继续到 Agent 检查，实际 %d，body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("invalid json rejected", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/v1/nginx/reload", strings.NewReader("{"))
		req = req.WithContext(applyRequestID(req))
		rec := httptest.NewRecorder()

		server.handleNginxReload(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("非法 JSON 应返回 400，实际 %d，body: %s", rec.Code, rec.Body.String())
		}
	})
}

func applyRequestID(req *http.Request) context.Context {
	return middleware.WithRequestID(req.Context(), "req_test_handler")
}
