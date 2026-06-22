package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
)

type sampleRequest struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func newTestReq(body string) *http.Request {
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := middleware.WithRequestID(req.Context(), "req_test")
	req = req.WithContext(ctx)
	return req
}

func parseResponse(t *testing.T, rec *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	return resp
}

func TestDecodeJSON_ValidBody(t *testing.T) {
	req := newTestReq(`{"name":"test","value":42}`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSON(rec, req, &dst)

	if !ok {
		t.Fatal("期望 DecodeJSON 返回 true")
	}
	if dst.Name != "test" {
		t.Errorf("name 期望 test，实际 %s", dst.Name)
	}
	if dst.Value != 42 {
		t.Errorf("value 期望 42，实际 %d", dst.Value)
	}
}

func TestDecodeJSON_UnknownField(t *testing.T) {
	req := newTestReq(`{"name":"test","value":1,"extra":true}`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSON(rec, req, &dst)

	if ok {
		t.Fatal("期望 DecodeJSON 对未知字段返回 false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
	resp := parseResponse(t, rec)
	if resp.Error == nil || resp.Error.Code != app.ErrBadRequest {
		t.Errorf("error.code 期望 %s", app.ErrBadRequest)
	}
}

func TestNginxDetectRequestRejectsNginxBin(t *testing.T) {
	req := newTestReq(`{"nginx_bin":"/www/wwwroot/site/pwn.sh"}`)
	rec := httptest.NewRecorder()

	var dst nginxDetectRequest
	ok := DecodeJSON(rec, req, &dst)

	if ok {
		t.Fatal("nginx detect API 不应再接收 nginx_bin 参数")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
}

func TestDecodeJSON_MultipleJSONValues(t *testing.T) {
	req := newTestReq(`{"name":"a","value":1}{"name":"b","value":2}`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSON(rec, req, &dst)

	if ok {
		t.Fatal("期望 DecodeJSON 对多个 JSON 值返回 false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
}

func TestDecodeJSON_EmptyBody(t *testing.T) {
	req := newTestReq("")
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSON(rec, req, &dst)

	if ok {
		t.Fatal("期望 DecodeJSON 对空 body 返回 false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
}

func TestDecodeJSON_InvalidJSON(t *testing.T) {
	req := newTestReq(`{invalid}`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSON(rec, req, &dst)

	if ok {
		t.Fatal("期望 DecodeJSON 对非法 JSON 返回 false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
}

func TestDecodeJSONOptional_ValidBody(t *testing.T) {
	req := newTestReq(`{"name":"opt","value":7}`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSONOptional(rec, req, &dst)

	if !ok {
		t.Fatal("期望 DecodeJSONOptional 返回 true")
	}
	if dst.Name != "opt" || dst.Value != 7 {
		t.Errorf("解码结果不正确: %+v", dst)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("状态码期望 200（不写响应），实际 %d", rec.Code)
	}
}

func TestDecodeJSONOptional_EmptyBody(t *testing.T) {
	req := newTestReq("")
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSONOptional(rec, req, &dst)

	if !ok {
		t.Fatal("期望 DecodeJSONOptional 对空 body 返回 true")
	}
	if dst.Name != "" || dst.Value != 0 {
		t.Errorf("空 body 应保持零值，实际 %+v", dst)
	}
}

func TestDecodeJSONOptional_InvalidJSON(t *testing.T) {
	req := newTestReq(`{bad`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSONOptional(rec, req, &dst)

	if ok {
		t.Fatal("期望 DecodeJSONOptional 对非法 JSON 返回 false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
	resp := parseResponse(t, rec)
	if resp.Error == nil || resp.Error.Code != app.ErrBadRequest {
		t.Errorf("error.code 期望 %s", app.ErrBadRequest)
	}
}

func TestDecodeJSONOptional_UnknownField(t *testing.T) {
	req := newTestReq(`{"name":"x","value":1,"surprise":true}`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSONOptional(rec, req, &dst)

	if ok {
		t.Fatal("期望 DecodeJSONOptional 对未知字段返回 false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
}

func TestDecodeJSONOptional_MultipleJSONValues(t *testing.T) {
	req := newTestReq(`{"name":"a","value":1}{"name":"b","value":2}`)
	rec := httptest.NewRecorder()

	var dst sampleRequest
	ok := DecodeJSONOptional(rec, req, &dst)

	if ok {
		t.Fatal("期望 DecodeJSONOptional 对多个 JSON 值返回 false")
	}
	if rec.Code != http.StatusBadRequest {
		t.Errorf("状态码期望 400，实际 %d", rec.Code)
	}
}
