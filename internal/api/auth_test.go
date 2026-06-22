// api 包集成测试 — setup/login/logout/me/CSRF/限流
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/captcha"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// setupTestAdmin 初始化测试管理员，返回 server 和 CSRF Token
func setupTestAdmin(t *testing.T, server *Server) {
	t.Helper()
	if err := server.authSvc.SetupAdmin("admin", "Test-password-123"); err != nil {
		t.Fatalf("setup admin 失败: %v", err)
	}
}

func doLogin(server *Server, username, password string) *httptest.ResponseRecorder {
	body := `{"username":"` + username + `","password":"` + password + `"}`
	req := httptest.NewRequest("POST", apiTestPath(server, "/auth/login"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func apiTestPath(server *Server, path string) string {
	return "/api/v1/" + server.GateSecretToken() + path
}

func newCaptchaTestServer(t *testing.T) *Server {
	t.Helper()
	server := newTestServerWithAgent(t)
	server.cfg.API.Captcha = app.CaptchaConfig{
		Provider:             "turnstile",
		SiteKey:              "site-key-for-test",
		SecretKey:            "secret-key-for-test",
		TriggerAfterFailures: 1,
	}
	server.captchaSvc = captcha.NewService(
		server.cfg.API.Captcha.Provider,
		server.cfg.API.Captcha.SecretKey,
		server.cfg.API.Captcha.SiteKey,
		server.cfg.API.Captcha.TriggerAfterFailures,
	)
	return server
}

func enableTestTOTP(t *testing.T, server *Server) {
	t.Helper()
	if err := repo.NewAdminRepo(server.db).UpdateTOTP("JBSWY3DPEHPK3PXP", true, "[]"); err != nil {
		t.Fatalf("启用测试 2FA 失败: %v", err)
	}
}

func parseTempToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("登录响应 data 应为对象: %#v", resp.Data)
	}
	token, _ := data["temp_token"].(string)
	if token == "" {
		t.Fatalf("登录响应应包含 temp_token: %#v", data)
	}
	return token
}

// parseLoginResponse 解析登录响应，提取 csrf_token 和 cookie
func parseLoginResponse(t *testing.T, rec *httptest.ResponseRecorder) (csrfToken, sessionCookie string) {
	t.Helper()

	for _, c := range rec.Result().Cookies() {
		if c.Name == "openrest_session" {
			sessionCookie = c.Value
		}
		if c.Name == "csrf-token" {
			csrfToken = c.Value
		}
	}
	return csrfToken, sessionCookie
}

// TestSetupAdmin_Success 测试成功初始化管理员
func TestSetupAdmin_Success(t *testing.T) {
	server := newTestServerWithAgent(t)

	body := `{"username":"admin","password":"Test-password-123"}`
	req := httptest.NewRequest("POST", apiTestPath(server, "/setup/admin"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("期望 201，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.Success {
		t.Error("响应 success 应为 true")
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("data 应为对象")
	}
	if data["username"] != "admin" {
		t.Errorf("username 期望 admin，实际 %v", data["username"])
	}
}

// TestSetupAdmin_WithCaptchaSettings 测试初始化管理员时携带 CAPTCHA 配置
func TestSetupAdmin_WithCaptchaSettings(t *testing.T) {
	server := newCaptchaTestServer(t)

	body := `{"username":"admin","password":"Test-password-123","captcha_provider":"turnstile","captcha_site_key":"site-key-for-test","captcha_secret_key":"secret-key-for-test","captcha_trigger_after_failures":2}`
	req := httptest.NewRequest("POST", apiTestPath(server, "/setup/admin"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("携带 CAPTCHA 配置的初始化应返回 201，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

// TestSetupAdmin_Duplicate 测试重复初始化管理员
func TestSetupAdmin_Duplicate(t *testing.T) {
	server := newTestServerWithAgent(t)
	setupTestAdmin(t, server)

	// 再次尝试初始化
	body := `{"username":"admin2","password":"another-password"}`
	req := httptest.NewRequest("POST", apiTestPath(server, "/setup/admin"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("重复初始化应返回 409，实际 %d", rec.Code)
	}
}

// TestSetupAdmin_ValidationErrors 测试参数校验错误
func TestSetupAdmin_ValidationErrors(t *testing.T) {
	server := newTestServerWithAgent(t)

	tests := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"空用户名", `{"username":"","password":"Test-pass123"}`, http.StatusUnprocessableEntity},
		{"空密码", `{"username":"admin","password":""}`, http.StatusUnprocessableEntity},
		{"密码太短", `{"username":"admin","password":"1234567"}`, http.StatusUnprocessableEntity},
		{"无请求体", ``, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", apiTestPath(server, "/setup/admin"), strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Errorf("期望 %d，实际 %d, body: %s", tt.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestLogin_Success 测试登录成功
func TestLogin_Success(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	rec := doLogin(server, "admin", "Test-password-123")

	if rec.Code != http.StatusOK {
		t.Errorf("登录成功应返回 200，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	csrfToken, sessionCookie := parseLoginResponse(t, rec)
	if csrfToken == "" {
		t.Error("登录响应应包含 csrf_token cookie")
	}
	if sessionCookie == "" {
		t.Error("登录响应应设置 session cookie")
	}
	if len(csrfToken) != 64 {
		t.Errorf("CSRF token 长度期望 64，实际 %d", len(csrfToken))
	}
}

// TestLogin_WrongPassword 测试密码错误
func TestLogin_WrongPassword(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	rec := doLogin(server, "admin", "wrong-password")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("密码错误应返回 401，实际 %d", rec.Code)
	}
}

// TestLogin_NonexistentUser 测试不存在的用户
func TestLogin_NonexistentUser(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	rec := doLogin(server, "nonexistent", "password")

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("不存在的用户应返回 401，实际 %d", rec.Code)
	}
}

// TestLogin_EmptyFields 测试空字段
func TestLogin_EmptyFields(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	tests := []struct {
		name string
		body string
	}{
		{"空用户名", `{"username":"","password":"password"}`},
		{"空密码", `{"username":"admin","password":""}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", apiTestPath(server, "/auth/login"), strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("空字段应返回 422，实际 %d", rec.Code)
			}
		})
	}
}

func TestCaptchaConfig_NotRequired_HidesProviderAndSiteKey(t *testing.T) {
	server := newCaptchaTestServer(t)

	req := httptest.NewRequest("GET", apiTestPath(server, "/auth/captcha-config"), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("captcha-config 应返回 200，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data 应为对象: %#v", resp.Data)
	}
	if required, _ := data["required"].(bool); required {
		t.Fatal("未触发时 required 应为 false")
	}
	if _, exists := data["provider"]; exists {
		t.Fatal("未触发时不应返回 provider")
	}
	if _, exists := data["site_key"]; exists {
		t.Fatal("未触发时不应返回 site_key")
	}
}

func TestCaptchaConfig_Required_ReturnsProviderAndSiteKey(t *testing.T) {
	server := newCaptchaTestServer(t)
	setupTestAdmin(t, server)
	_ = doLogin(server, "admin", "wrong-password")

	req := httptest.NewRequest("GET", apiTestPath(server, "/auth/captcha-config"), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("captcha-config 应返回 200，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data 应为对象: %#v", resp.Data)
	}
	if required, _ := data["required"].(bool); !required {
		t.Fatal("触发后 required 应为 true")
	}
	if provider, _ := data["provider"].(string); provider != "turnstile" {
		t.Fatalf("provider 期望 turnstile，实际 %q", provider)
	}
	if siteKey, _ := data["site_key"].(string); siteKey != "site-key-for-test" {
		t.Fatalf("site_key 期望测试值，实际 %q", siteKey)
	}
}

func TestLogin_CaptchaFailed_HidesReasonDetails(t *testing.T) {
	server := newCaptchaTestServer(t)
	setupTestAdmin(t, server)
	_ = doLogin(server, "admin", "wrong-password")

	rec := doLogin(server, "admin", "Test-password-123")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺少 CAPTCHA 应返回 400，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Error == nil {
		t.Fatal("错误响应应包含 error")
	}
	if resp.Error.Code != "CAPTCHA_FAILED" {
		t.Fatalf("错误码期望 CAPTCHA_FAILED，实际 %q", resp.Error.Code)
	}
	if resp.Error.Details != nil {
		t.Fatalf("不应返回第三方或内部 reason 详情: %#v", resp.Error.Details)
	}
}

// TestLogout 测试退出登录
func TestLogout(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	// 先登录
	loginRec := doLogin(server, "admin", "Test-password-123")
	csrfToken, sessionCookie := parseLoginResponse(t, loginRec)

	// 退出登录（需要 CSRF token）
	req := httptest.NewRequest("POST", apiTestPath(server, "/auth/logout"), nil)
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("退出登录应返回 200，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	// 验证 Cookie 被清除（MaxAge=-1）
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "openrest_session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("退出登录应清除 session cookie")
	}
}

// TestMe_Authenticated 测试已登录状态获取用户信息
func TestMe_Authenticated(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	// 先登录
	loginRec := doLogin(server, "admin", "Test-password-123")
	_, sessionCookie := parseLoginResponse(t, loginRec)

	// 获取用户信息
	req := httptest.NewRequest("GET", apiTestPath(server, "/auth/me"), nil)
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("me 应返回 200，实际 %d", rec.Code)
	}

	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("data 应为对象")
	}
	loggedIn, _ := data["authenticated"].(bool)
	if !loggedIn {
		t.Error("已登录时 authenticated 应为 true")
	}
	if data["username"] != "admin" {
		t.Errorf("username 期望 admin，实际 %v", data["username"])
	}
}

// TestMe_Unauthenticated 测试未登录状态
func TestMe_Unauthenticated(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest("GET", apiTestPath(server, "/auth/me"), nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("me 未登录也应返回 200，实际 %d", rec.Code)
	}

	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("data 应为对象")
	}
	loggedIn, _ := data["authenticated"].(bool)
	if loggedIn {
		t.Error("未登录时 authenticated 应为 false")
	}
}

// TestCSRFProtection 测试 CSRF 保护
func TestCSRFProtection(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	// 先登录获取 session 和 csrf token
	loginRec := doLogin(server, "admin", "Test-password-123")
	csrfToken, sessionCookie := parseLoginResponse(t, loginRec)

	// 不带 CSRF token 的 logout 应被拦截
	req := httptest.NewRequest("POST", apiTestPath(server, "/auth/logout"), nil)
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("缺少 CSRF token 的写操作应返回 403，实际 %d", rec.Code)
	}

	// 带 CSRF token 的 logout 应成功
	req2 := httptest.NewRequest("POST", apiTestPath(server, "/auth/logout"), nil)
	req2.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	req2.Header.Set("X-CSRF-Token", csrfToken)
	rec2 := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("带正确 CSRF token 应返回 200，实际 %d", rec2.Code)
	}
}

// TestRateLimiting 测试登录限流
func TestRateLimiting(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	// 连续登录失败 5 次（限流阈值）
	for i := 0; i < 5; i++ {
		rec := doLogin(server, "admin", "wrong-password")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("第 %d 次失败登录应返回 401，实际 %d", i+1, rec.Code)
		}
	}

	// 第 6 次应被限流
	rec := doLogin(server, "admin", "Test-password-123")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("限流后应返回 429，实际 %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestLogin2FA_TempTokenContextBinding(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	enableTestTOTP(t, server)

	loginRec := doLogin(server, "admin", "Test-password-123")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("启用 2FA 后密码登录应返回 200，实际 %d, body: %s", loginRec.Code, loginRec.Body.String())
	}
	tempToken := parseTempToken(t, loginRec)

	body := `{"temp_token":"` + tempToken + `","code":"000000"}`
	req := httptest.NewRequest("POST", apiTestPath(server, "/auth/login/2fa"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "different-ua")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("不同 User-Agent 使用 temp_token 应返回 401，实际 %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestLogin2FA_FailuresAffectRateLimit(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	enableTestTOTP(t, server)

	loginRec := doLogin(server, "admin", "Test-password-123")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("启用 2FA 后密码登录应返回 200，实际 %d, body: %s", loginRec.Code, loginRec.Body.String())
	}
	tempToken := parseTempToken(t, loginRec)

	// 2FA 二阶段失败也要写入 IP 限流，避免绕过密码登录的失败计数。
	for i := 0; i < 5; i++ {
		body := `{"temp_token":"` + tempToken + `","code":"000000"}`
		req := httptest.NewRequest("POST", apiTestPath(server, "/auth/login/2fa"), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次 2FA 失败应返回 400/401，实际 %d, body: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := doLogin(server, "admin", "Test-password-123")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("2FA 失败达到阈值后登录应被限流，实际 %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestLoginRecover_FailuresAffectRateLimit(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)
	enableTestTOTP(t, server)

	loginRec := doLogin(server, "admin", "Test-password-123")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("启用 2FA 后密码登录应返回 200，实际 %d, body: %s", loginRec.Code, loginRec.Body.String())
	}
	tempToken := parseTempToken(t, loginRec)

	for i := 0; i < 5; i++ {
		body := `{"temp_token":"` + tempToken + `","recovery_code":"wrong-code"}`
		req := httptest.NewRequest("POST", apiTestPath(server, "/auth/login/recover"), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次恢复码失败应返回 401，实际 %d, body: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := doLogin(server, "admin", "Test-password-123")
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("恢复码失败达到阈值后登录应被限流，实际 %d, body: %s", rec.Code, rec.Body.String())
	}
}

func TestTwoFADisableFailuresAreSessionRateLimited(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	loginRec := doLogin(server, "admin", "Test-password-123")
	if loginRec.Code != http.StatusOK {
		t.Fatalf("密码登录应返回 200，实际 %d, body: %s", loginRec.Code, loginRec.Body.String())
	}
	csrfToken, sessionCookie := parseLoginResponse(t, loginRec)
	if csrfToken == "" || sessionCookie == "" {
		t.Fatalf("测试登录应返回 session 与 CSRF cookie")
	}
	enableTestTOTP(t, server)

	for i := 0; i < 5; i++ {
		body := `{"code":"000000"}`
		req := httptest.NewRequest("POST", apiTestPath(server, "/auth/2fa/disable"), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", csrfToken)
		req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("第 %d 次错误验证码应返回 400/429，实际 %d, body: %s", i+1, rec.Code, rec.Body.String())
		}
	}

	body := `{"code":"000000"}`
	req := httptest.NewRequest("POST", apiTestPath(server, "/auth/2fa/disable"), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrfToken)
	req.AddCookie(&http.Cookie{Name: "openrest_session", Value: sessionCookie})
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("2FA 敏感操作失败达到阈值后应返回 429，实际 %d, body: %s", rec.Code, rec.Body.String())
	}
}

// TestProtectedRoutesRequireAuth 测试需要认证的路由返回 401
func TestProtectedRoutesRequireAuth(t *testing.T) {
	server := newTestServer(t)
	setupTestAdmin(t, server)

	// 未认证时访问需要认证的路由
	routes := []struct {
		method string
		path   string
	}{
		{"GET", apiTestPath(server, "/system/overview")},
		{"GET", apiTestPath(server, "/sites")},
		{"GET", apiTestPath(server, "/operations")},
	}

	for _, route := range routes {
		t.Run(route.method+"_"+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("未认证应返回 401，实际 %d", rec.Code)
			}
		})
	}
}
