// middleware 包测试 — auth 和 csrf 中间件
package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthenticateWithNoCookie 测试没有 Cookie 时中间件放行
func TestAuthenticateWithNoCookie(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		// 验证 context 中没有 session
		session := GetSession(r.Context())
		if session != nil {
			t.Error("没有 Cookie 时 session 应为 nil")
		}
	})

	// 使用 nil authSvc 表示不会实际调用
	handler := Authenticate(nil)(next)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("next handler 应被调用")
	}
}

// TestRequireAuthBlocksUnauthenticated 测试 RequireAuth 拦截未认证请求
func TestRequireAuthBlocksUnauthenticated(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := RequireAuth(next)

	req := httptest.NewRequest("POST", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("未认证时 next handler 不应被调用")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("状态码期望 401，实际 %d", rec.Code)
	}
}

// TestRequireAuthPassesAuthenticated 测试 RequireAuth 放行已认证请求
func TestRequireAuthPassesAuthenticated(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		session := GetSession(r.Context())
		if session == nil {
			t.Error("已认证请求 context 中应有 session")
		}
	})

	handler := RequireAuth(next)

	// 注入模拟的 session 信息到 context
	req := httptest.NewRequest("POST", "/test", nil)
	ctx := context.WithValue(req.Context(), SessionKey, &SessionInfo{
		ID:            "test-session-id",
		CSRFTokenHash: "test-csrf-hash",
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("已认证时 next handler 应被调用")
	}
}

// TestCSRFProtectAllowsGET 测试 CSRF 中间件放行 GET 请求
func TestCSRFProtectAllowsGET(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := CSRFProtect()(next)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("GET 请求应被放行")
	}
}

// TestCSRFProtectBlocksPOSTWithoutToken 测试 CSRF 拦截缺少 Token 的 POST
func TestCSRFProtectBlocksPOSTWithoutToken(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := CSRFProtect()(next)

	req := httptest.NewRequest("POST", "/test", nil)
	// 注入已认证的 session（模拟已登录用户）
	ctx := context.WithValue(req.Context(), SessionKey, &SessionInfo{
		ID:            "test-session-id",
		CSRFTokenHash: "test-csrf-hash",
	})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("缺少 CSRF token 的 POST 不应被放行")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("状态码期望 403，实际 %d", rec.Code)
	}
}

// TestCSRFProtectAllowsPOSTWithoutSession 测试 CSRF 放行未认证的 POST
func TestCSRFProtectAllowsPOSTWithoutSession(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := CSRFProtect()(next)

	req := httptest.NewRequest("POST", "/test", nil)
	// 没有注入 session（未登录状态）
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("未登录的 POST 应被放行（auth 中间件负责拦截）")
	}
}

// TestLoginRateLimiter 测试限流器
func TestLoginRateLimiter(t *testing.T) {
	limiter := NewLoginRateLimiter(3, 60*1e9) // 3 次失败/分钟

	ip := "192.168.1.1"

	// 前 3 次检查应通过
	for i := 0; i < 3; i++ {
		if !limiter.Check(ip) {
			t.Errorf("第 %d 次检查应通过", i+1)
		}
		limiter.RecordFail(ip)
	}

	// 第 4 次应被限流
	if limiter.Check(ip) {
		t.Error("超过限制后应被限流")
	}

	// 不同 IP 不受限流影响
	if !limiter.Check("192.168.1.2") {
		t.Error("不同 IP 不应受限流影响")
	}
}

// TestLoginRateLimitMiddleware 测试限流中间件
func TestLoginRateLimitMiddleware(t *testing.T) {
	limiter := NewLoginRateLimiter(2, 60*1e9)

	called := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
	})

	limiter.RecordFail("1.2.3.4")
	limiter.RecordFail("1.2.3.4")

	req := httptest.NewRequest("POST", "/login", nil)
	ctx := context.WithValue(req.Context(), RealIPKey, "1.2.3.4")
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	handler := LoginRateLimitMiddleware(limiter)(next)
	handler.ServeHTTP(rec, req)

	if called != 0 {
		t.Error("被限流的请求不应到达 handler")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("被限流应返回 429，实际 %d", rec.Code)
	}
}

// TestGetSessionNilContext 测试 GetSession 对 nil context 的安全处理
func TestGetSessionNilContext(t *testing.T) {
	session := GetSession(nil)
	if session != nil {
		t.Error("nil context 应返回 nil session")
	}
}

// TestGetSessionEmptyContext 测试 GetSession 对空 context 的处理
func TestGetSessionEmptyContext(t *testing.T) {
	session := GetSession(context.Background())
	if session != nil {
		t.Error("没有 session 的 context 应返回 nil")
	}
}
