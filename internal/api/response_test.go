// api 包的统一响应结构测试
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/luoye663/nxpanel/internal/api/middleware"
)

// TestWriteOK 验证成功响应的 JSON 结构
func TestWriteOK(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	// 模拟 RequestID 中间件注入 request_id
	ctx := middleware.WithRequestID(req.Context(), "req_test123")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	WriteOK(rec, req, map[string]string{"key": "value"})

	// 验证 HTTP 状态码
	if rec.Code != http.StatusOK {
		t.Errorf("状态码期望 200，实际 %d", rec.Code)
	}

	// 验证 Content-Type
	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type 期望 application/json; charset=utf-8，实际 %s", ct)
	}

	// 验证 JSON 响应体
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应 JSON 失败: %v", err)
	}

	if !resp.Success {
		t.Error("成功响应的 success 应为 true")
	}
	if resp.RequestID != "req_test123" {
		t.Errorf("request_id 期望 req_test123，实际 %s", resp.RequestID)
	}
	if resp.Error != nil {
		t.Error("成功响应的 error 应为 nil")
	}
}

// TestWriteCreated 验证 201 创建成功响应
func TestWriteCreated(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	ctx := middleware.WithRequestID(req.Context(), "req_create")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	WriteCreated(rec, req, map[string]string{"id": "site_xxx"})

	if rec.Code != http.StatusCreated {
		t.Errorf("状态码期望 201，实际 %d", rec.Code)
	}

	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.Success {
		t.Error("创建成功响应的 success 应为 true")
	}
}

// TestWriteError 验证错误响应的 JSON 结构
func TestWriteError(t *testing.T) {
	req := httptest.NewRequest("POST", "/test", nil)
	ctx := middleware.WithRequestID(req.Context(), "req_err")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	WriteError(rec, req, http.StatusNotFound,
		"NOT_FOUND",
		"资源不存在",
		map[string]any{"resource": "site_xxx"},
	)

	// 验证 HTTP 状态码
	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码期望 404，实际 %d", rec.Code)
	}

	// 验证 JSON 结构
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应 JSON 失败: %v", err)
	}

	if resp.Success {
		t.Error("错误响应的 success 应为 false")
	}
	if resp.Data != nil {
		t.Error("错误响应的 data 应为 nil")
	}
	if resp.Error == nil {
		t.Fatal("错误响应的 error 不应为 nil")
	}
	if resp.Error.Code != "NOT_FOUND" {
		t.Errorf("error.code 期望 NOT_FOUND，实际 %s", resp.Error.Code)
	}
	if resp.Error.Message != "资源不存在" {
		t.Errorf("error.message 不正确: %s", resp.Error.Message)
	}
	if resp.Error.Details["resource"] != "site_xxx" {
		t.Error("error.details 应包含 resource 字段")
	}
}

// TestWriteError_WithoutDetails 验证无 details 的错误响应
func TestWriteError_WithoutDetails(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	ctx := middleware.WithRequestID(req.Context(), "req_simple_err")
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	WriteError(rec, req, http.StatusInternalServerError,
		"INTERNAL_ERROR",
		"内部错误",
		nil,
	)

	var resp Response
	json.NewDecoder(rec.Body).Decode(&resp)

	if resp.Error.Details != nil {
		// omitempty 应该让 details 为 nil（JSON 中不出现）
		t.Error("details 为 nil 时不应出现在 JSON 中")
	}
}
