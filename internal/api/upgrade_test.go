// api 包集成测试 — 升级检查触发端点
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestUpgradeCheckTriggerRequiresAuth 未认证调用 POST /system/upgrade/check 应返回 401。
func TestUpgradeCheckTriggerRequiresAuth(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	req := httptest.NewRequest("POST", apiTestPath(server, "/system/upgrade/check"), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("未认证应返回 401，实际 %d", rec.Code)
	}
}

// TestUpgradeCheckTriggerRequiresCSRF 缺少 CSRF token 的 POST 应被拦截。
func TestUpgradeCheckTriggerRequiresCSRF(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	loginRec := doLogin(server, "admin", "Test-password-123")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("登录应返回 200，实际 %d", loginRec.Code)
	}
	_, sessionCookie := parseLoginResponse(t, loginRec)

	req := httptest.NewRequest("POST", apiTestPath(server, "/system/upgrade/check"), nil)
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("缺少 CSRF token 的写操作应返回 403，实际 %d", rec.Code)
	}
}

// TestUpgradeCheckTriggerSuccess 带 CSRF 的认证请求应返回 200 与 UpgradeStatus 结构。
//
// 测试服务器 Upgrade 配置为零值（enabled=false），upgradeSvc 仍非 nil，
// CheckNow 走 disabled 分支直接返回缓存状态。
func TestUpgradeCheckTriggerSuccess(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	loginRec := doLogin(server, "admin", "Test-password-123")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("登录应返回 200，实际 %d", loginRec.Code)
	}
	csrfToken, sessionCookie := parseLoginResponse(t, loginRec)

	req := httptest.NewRequest("POST", apiTestPath(server, "/system/upgrade/check"), nil)
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	req.Header.Set("X-CSRF-Token", csrfToken)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("带 CSRF 的认证请求应返回 200，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败：%v", err)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("响应 data 应为对象：%#v", resp.Data)
	}
	if _, ok := data["current_version"].(string); !ok {
		t.Errorf("响应应包含 current_version 字符串字段：%#v", data)
	}
	if data["has_upgrade"] != false {
		t.Errorf("测试服务器未配置升级检测，has_upgrade 应为 false：%#v", data)
	}
}

// TestUpgradeCheckGetAndPostBothMounted GET 与 POST 路由都应被挂载到受保护组。
func TestUpgradeCheckGetAndPostBothMounted(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	loginRec := doLogin(server, "admin", "Test-password-123")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("登录应返回 200，实际 %d", loginRec.Code)
	}
	csrfToken, sessionCookie := parseLoginResponse(t, loginRec)

	cases := []struct {
		method string
		path   string
	}{
		{"GET", "/system/upgrade"},
		{"POST", "/system/upgrade/check"},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, apiTestPath(server, c.path), nil)
		req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
		if c.method == "POST" {
			req.Header.Set("X-CSRF-Token", csrfToken)
		}
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s %s 应返回 200，实际 %d, body: %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}
