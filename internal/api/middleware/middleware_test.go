// middleware 包的中间件测试
package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRequestID_GeneratesNewID 验证没有 X-Request-ID 头时自动生成 ID
func TestRequestID_GeneratesNewID(t *testing.T) {
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := GetRequestID(r.Context())
		if rid == "" {
			t.Error("RequestID 中间件应生成非空的 request_id")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// 验证响应头中有 X-Request-ID
	respRID := rec.Header().Get("X-Request-ID")
	if respRID == "" {
		t.Error("响应头中应包含 X-Request-ID")
	}
}

// TestRequestID_PassthroughExisting 验证已有 X-Request-ID 头时透传
func TestRequestID_PassthroughExisting(t *testing.T) {
	existingID := "req_from_proxy_123"

	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := GetRequestID(r.Context())
		if rid != existingID {
			t.Errorf("应透传已有的 request_id，期望 %s，实际 %s", existingID, rid)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	respRID := rec.Header().Get("X-Request-ID")
	if respRID != existingID {
		t.Errorf("响应头 X-Request-ID 应为 %s，实际 %s", existingID, respRID)
	}
}

// TestRequestID_Uniqueness 验证连续请求生成不同的 request_id
func TestRequestID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)

	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := GetRequestID(r.Context())
		ids[rid] = true
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 50; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	// 50 个请求应该生成 50 个不同的 ID
	if len(ids) != 50 {
		t.Errorf("期望 50 个不同的 request_id，实际 %d 个", len(ids))
	}
}

// TestGetRequestID_EmptyContext 验证空 context 返回空字符串
func TestGetRequestID_EmptyContext(t *testing.T) {
	rid := GetRequestID(context.Background())
	if rid != "" {
		t.Errorf("空 context 应返回空字符串，实际 %s", rid)
	}
}

// TestRecoverer_NoPanic 验证正常请求不被 recoverer 影响
func TestRecoverer_NoPanic(t *testing.T) {
	handler := Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	// 注入 request_id（模拟 RequestID 中间件已执行）
	innerHandler := RequestID(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	innerHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("正常请求状态码应为 200，实际 %d", rec.Code)
	}
}

// TestRecoverer_PanicRecovered 验证 panic 被捕获并返回 JSON 错误
func TestRecoverer_PanicRecovered(t *testing.T) {
	panicMsg := "something went wrong"

	// 先 RequestID 再 Recoverer，这样 Recoverer 可以获取 request_id
	handler := RequestID(Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(panicMsg)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// 验证返回 500
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("panic 后状态码应为 500，实际 %d", rec.Code)
	}

	// 验证返回 JSON
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type 应为 JSON，实际 %s", ct)
	}

	// 验证响应头包含 request_id
	rid := rec.Header().Get("X-Request-ID")
	if rid == "" {
		t.Error("panic 恢复后响应头应包含 X-Request-ID")
	}

	// 验证 JSON 结构
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应 JSON 失败: %v", err)
	}

	if success, ok := resp["success"].(bool); ok && success {
		t.Error("panic 恢复后 success 应为 false")
	}

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("响应应包含 error 对象")
	}
	if errObj["code"] != "INTERNAL_ERROR" {
		t.Errorf("error.code 应为 INTERNAL_ERROR，实际 %v", errObj["code"])
	}
}

// TestRecoverer_PanicNil 验证 panic(nil) 也能被正确恢复
func TestRecoverer_PanicNil(t *testing.T) {
	handler := RequestID(Recoverer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(nil)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("panic(nil) 后状态码应为 500，实际 %d", rec.Code)
	}
}

// TestWithRequestID 验证 WithRequestID 辅助函数
func TestWithRequestID(t *testing.T) {
	ctx := WithRequestID(context.Background(), "test_rid")
	rid := GetRequestID(ctx)
	if rid != "test_rid" {
		t.Errorf("WithRequestID 注入后 GetRequestID 应返回 test_rid，实际 %s", rid)
	}
}
