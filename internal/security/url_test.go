// security 包测试 — upstream URL validator 测试
package security

import "testing"

func TestValidateUpstreamURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		// 合法 URL
		{"http localhost", "http://127.0.0.1:3000", false},
		{"https 域名", "https://backend.example.com", false},
		{"http 域名带端口", "http://backend.example.com:8080", false},
		{"http localhost 无端口", "http://localhost", false},
		{"http IP 无端口", "http://192.168.1.1", false},
		{"http 带路径", "http://127.0.0.1:3000/api", false},

		// 非法 URL
		{"空值", "", true},
		{"ftp 协议", "ftp://example.com", true},
		{"javascript 协议", "javascript://alert(1)", true},
		{"无 scheme", "127.0.0.1:3000", true},
		{"包含分号", "http://127.0.0.1:3000;evil", true},
		{"包含花括号左", "http://127.0.0.1:3000{", true},
		{"包含花括号右", "http://127.0.0.1:3000}", true},
		{"包含换行", "http://127.0.0.1:3000\n", true},
		{"包含回车", "http://127.0.0.1:3000\r", true},
		{"包含空字节", "http://127.0.0.1:3000\x00", true},
		{"无 host", "http://", true},
		{"非法端口", "http://127.0.0.1:abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpstreamURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateUpstreamURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateHostHeader(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		wantErr bool
	}{
		{"nginx 变量 $host", "$host", false},
		{"nginx 变量 $http_host", "$http_host", false},
		{"具体域名", "backend.example.com", false},
		{"空值", "", true},
		{"包含分号", "$host;", true},
		{"包含花括号", "$host{", true},
		{"包含换行", "$host\n", true},
		{"包含空字节", "$host\x00", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHostHeader(tt.header)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHostHeader(%q) error = %v, wantErr %v", tt.header, err, tt.wantErr)
			}
		})
	}
}
