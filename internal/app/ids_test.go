// app 包的 ID 生成测试
package app

import (
	"strings"
	"testing"
)

// TestNewID 验证 ID 生成格式和唯一性
func TestNewID(t *testing.T) {
	id := NewID("test")

	// 应该以 "test_" 前缀开头
	if !strings.HasPrefix(id, "test_") {
		t.Errorf("ID 应以 test_ 开头，实际: %s", id)
	}

	// 长度应合理（前缀 + 时间戳 hex + 下划线 + 8 hex 字符）
	parts := strings.SplitN(id, "_", 3)
	if len(parts) != 3 {
		t.Errorf("ID 格式应为 prefix_timestamp_random，实际: %s", id)
	}
}

// TestNewID_Uniqueness 验证连续生成的 ID 不重复
func TestNewID_Uniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := NewID("uniq")
		if ids[id] {
			t.Errorf("生成了重复的 ID: %s", id)
		}
		ids[id] = true
	}
}

// TestNewRequestID 验证 request_id 格式
func TestNewRequestID(t *testing.T) {
	id := NewRequestID()
	if !strings.HasPrefix(id, "req_") {
		t.Errorf("RequestID 应以 req_ 开头，实际: %s", id)
	}
}

// TestNewSiteID 验证 site_id 格式
func TestNewSiteID(t *testing.T) {
	id := NewSiteID()
	if !strings.HasPrefix(id, "site_") {
		t.Errorf("SiteID 应以 site_ 开头，实际: %s", id)
	}
}

// TestNewOperationID 验证 operation_id 格式
func TestNewOperationID(t *testing.T) {
	id := NewOperationID()
	if !strings.HasPrefix(id, "op_") {
		t.Errorf("OperationID 应以 op_ 开头，实际: %s", id)
	}
}

// TestNewSessionID 验证 session_id 格式（纯 hex，64 字符）
func TestNewSessionID(t *testing.T) {
	id := NewSessionID()
	if len(id) != 64 {
		t.Errorf("SessionID 长度应为 64（32 字节 hex），实际: %d", len(id))
	}
	// 应该全是 hex 字符
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("SessionID 应全部为 hex 小写字符，发现: %c", c)
			break
		}
	}
}

// TestNewCSRFToken 验证 CSRF Token 格式（纯 hex，64 字符）
func TestNewCSRFToken(t *testing.T) {
	token := NewCSRFToken()
	if len(token) != 64 {
		t.Errorf("CSRFToken 长度应为 64（32 字节 hex），实际: %d", len(token))
	}
}
