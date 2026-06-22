package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrustedRealIP(t *testing.T) {
	tests := []struct {
		name     string
		trusted  []string
		remote   string
		xff      string
		xri      string
		expected string
	}{
		{
			name:     "未配置可信代理时忽略转发头",
			remote:   "10.0.0.10:12345",
			xff:      "203.0.113.10",
			expected: "10.0.0.10",
		},
		{
			name:     "非可信来源伪造 XFF 不生效",
			trusted:  []string{"10.0.0.0/24"},
			remote:   "198.51.100.2:12345",
			xff:      "203.0.113.10",
			expected: "198.51.100.2",
		},
		{
			name:     "可信代理读取 XFF 中的客户端 IP",
			trusted:  []string{"10.0.0.0/24"},
			remote:   "10.0.0.10:12345",
			xff:      "203.0.113.10",
			expected: "203.0.113.10",
		},
		{
			name:     "多级代理链跳过右侧可信代理",
			trusted:  []string{"10.0.0.0/24", "172.16.0.0/12"},
			remote:   "10.0.0.10:12345",
			xff:      "203.0.113.10, 172.16.0.20, 10.0.0.10",
			expected: "203.0.113.10",
		},
		{
			name:     "非法 XFF 值被跳过",
			trusted:  []string{"10.0.0.0/24"},
			remote:   "10.0.0.10:12345",
			xff:      "bad-ip, 203.0.113.10",
			expected: "203.0.113.10",
		},
		{
			name:     "全部 XFF 都非法时回退到可信的 X-Real-IP",
			trusted:  []string{"10.0.0.0/24"},
			remote:   "10.0.0.10:12345",
			xff:      "bad-ip",
			xri:      "203.0.113.20",
			expected: "203.0.113.20",
		},
		{
			name:     "非法 X-Real-IP 被忽略",
			trusted:  []string{"10.0.0.0/24"},
			remote:   "10.0.0.10:12345",
			xri:      "bad-ip",
			expected: "10.0.0.10",
		},
		{
			name:     "全部代理链可信时取最左侧合法 IP",
			trusted:  []string{"10.0.0.0/24", "172.16.0.0/12"},
			remote:   "10.0.0.10:12345",
			xff:      "172.16.0.20, 10.0.0.10",
			expected: "172.16.0.20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := TrustedRealIP(tt.trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if ip := GetRealIP(r.Context()); ip != tt.expected {
					t.Fatalf("真实 IP 不符合预期，期望 %s，实际 %s", tt.expected, ip)
				}
			}))

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remote
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}
			handler.ServeHTTP(httptest.NewRecorder(), req)
		})
	}
}
