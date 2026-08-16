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
		{"Docker DNS 下划线", "http://service_api:8080", false},

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
		{"零端口", "http://127.0.0.1:0", true},
		{"超大端口", "http://127.0.0.1:65536", true},
		{"主机变量", "http://$host:8080", true},
		{"用户信息", "http://user@example.com", true},
		{"引号注入", "http://example.com\"", true},
		{"注释符注入", "http://example.com/#comment", true},
		{"反斜杠注入", `http://example.com/\evil`, true},
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
		{"nginx 变量 $proxy_host", "$proxy_host", false},
		{"具体域名", "backend.example.com", false},
		{"空值", "", true},
		{"包含分号", "$host;", true},
		{"包含花括号", "$host{", true},
		{"包含换行", "$host\n", true},
		{"包含空字节", "$host\x00", true},
		{"未知变量", "$upstream_addr", true},
		{"空格注入", "example.com bad", true},
		{"合法端口", "backend.example.com:8443", false},
		{"非法端口", "backend.example.com:65536", true},
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

func TestValidateProxyTLSAndLocationValues(t *testing.T) {
	validNames := []string{"origin.example.com", "192.0.2.10", "2001:db8::1"}
	for _, value := range validNames {
		if err := ValidateProxySSLServerName(value); err != nil {
			t.Fatalf("valid SNI %q: %v", value, err)
		}
	}
	for _, value := range []string{"", "$proxy_host", "origin.example.com:443", "bad;name", "bad name", "-bad.example"} {
		if err := ValidateProxySSLServerName(value); err == nil {
			t.Fatalf("invalid SNI accepted: %q", value)
		}
	}
	if err := ValidateProxySSLServerName("service_api"); err == nil {
		t.Fatal("SNI must continue rejecting underscores")
	}
	if err := ValidateProxySSLTrustedCertificate("/etc/ssl/certs/ca.pem"); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"ca.pem", "/etc/ca.pem; include /tmp/x", "/etc/ca\".pem", "/etc/ca file.pem", `/etc/ca\file.pem`, "/etc/ca#file.pem"} {
		if err := ValidateProxySSLTrustedCertificate(value); err == nil {
			t.Fatalf("invalid CA path accepted: %q", value)
		}
	}
	for _, value := range []string{"/api", "/assets-v1/"} {
		if err := ValidateProxyLocationPath(value); err != nil {
			t.Fatalf("valid location %q: %v", value, err)
		}
	}
	for _, value := range []string{"api", "/api {", "/api;", "/api bad", "/api\"", "/api#comment", `/api\bad`} {
		if err := ValidateProxyLocationPath(value); err == nil {
			t.Fatalf("invalid location accepted: %q", value)
		}
	}
}
