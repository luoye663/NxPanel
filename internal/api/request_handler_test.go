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
