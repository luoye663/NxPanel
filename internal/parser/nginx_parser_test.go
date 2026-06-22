package parser

import (
	"testing"
)

const fakeNginxDump = `nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
nginx: configuration file /etc/nginx/nginx.conf test is successful
# configuration file /etc/nginx/nginx.conf:
user nginx;
worker_processes auto;
http {
    include /etc/nginx/mime.types;
}
# configuration file /etc/nginx/conf.d/legacy.conf:
server {
    listen 80;
    server_name old.example.com www.old.example.com;
    root /var/www/old;
    access_log /var/log/nginx/old.access.log;
    error_log /var/log/nginx/old.error.log;
    location / {
        try_files $uri $uri/ =404;
    }
}
# configuration file /etc/nginx/conf.d/ssl-site.conf:
server {
    listen 443 ssl;
    server_name ssl.example.com;
    root /var/www/ssl;
    ssl_certificate /etc/ssl/cert.pem;
    ssl_certificate_key /etc/ssl/key.pem;
}
`

func TestParseNginxDump_Count(t *testing.T) {
	servers := ParseNginxDump(fakeNginxDump)
	if len(servers) != 2 {
		t.Fatalf("期望解析出 2 个 server block，实际 %d", len(servers))
	}
}

func TestParseNginxDump_LegacyServer(t *testing.T) {
	servers := ParseNginxDump(fakeNginxDump)
	s := servers[0]

	if s.SourceFile != "/etc/nginx/conf.d/legacy.conf" {
		t.Errorf("SourceFile 期望 /etc/nginx/conf.d/legacy.conf，实际 %s", s.SourceFile)
	}
	if len(s.Listen) != 1 || s.Listen[0] != "80" {
		t.Errorf("Listen 期望 [80]，实际 %v", s.Listen)
	}
	if len(s.ServerNames) != 2 {
		t.Errorf("ServerNames 期望 2 个，实际 %d: %v", len(s.ServerNames), s.ServerNames)
	}
	if s.RootPath != "/var/www/old" {
		t.Errorf("RootPath 期望 /var/www/old，实际 %s", s.RootPath)
	}
	if s.AccessLogPath != "/var/log/nginx/old.access.log" {
		t.Errorf("AccessLogPath 不正确: %s", s.AccessLogPath)
	}
	if s.ErrorLogPath != "/var/log/nginx/old.error.log" {
		t.Errorf("ErrorLogPath 不正确: %s", s.ErrorLogPath)
	}
}

func TestParseNginxDump_SSLServer(t *testing.T) {
	servers := ParseNginxDump(fakeNginxDump)
	s := servers[1]

	if len(s.Listen) != 1 || s.Listen[0] != "443" {
		t.Errorf("Listen 期望 [443]（ssl 是单独参数），实际 %v", s.Listen)
	}
	if s.RootPath != "/var/www/ssl" {
		t.Errorf("RootPath 期望 /var/www/ssl，实际 %s", s.RootPath)
	}
}

func TestParseNginxDump_EmptyInput(t *testing.T) {
	servers := ParseNginxDump("")
	if len(servers) != 0 {
		t.Errorf("空输入应返回 0 个 server，实际 %d", len(servers))
	}
}

func TestParseNginxDump_NoServerBlocks(t *testing.T) {
	input := `nginx: the configuration file /etc/nginx/nginx.conf syntax is ok
# configuration file /etc/nginx/nginx.conf:
user nginx;
`
	servers := ParseNginxDump(input)
	if len(servers) != 0 {
		t.Errorf("无 server block 应返回 0 个，实际 %d", len(servers))
	}
}

func TestParseNginxDump_NestedServerBlocks(t *testing.T) {
	input := `# configuration file /etc/nginx/conf.d/nested.conf:
server {
    listen 80;
    server_name nested.example.com;
    root /var/www/nested;
    location / {
        try_files $uri $uri/ =404;
    }
    location /api/ {
        proxy_pass http://127.0.0.1:3000;
    }
}
`
	servers := ParseNginxDump(input)
	if len(servers) != 1 {
		t.Fatalf("期望 1 个 server block，实际 %d", len(servers))
	}
	s := servers[0]
	if len(s.ServerNames) != 1 || s.ServerNames[0] != "nested.example.com" {
		t.Errorf("ServerNames 不正确: %v", s.ServerNames)
	}
	if s.RootPath != "/var/www/nested" {
		t.Errorf("RootPath 不正确: %s", s.RootPath)
	}
}

func TestIsServerKeyword(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"server", true},
		{"server {", true},
		{"server{", true},
		{"server\t{", true},
		{"server  {", true},
		{"server_name", false},
		{"server_name a.com;", false},
		{"server_tokens off;", false},
		{"server_names_hash_bucket_size 64;", false},
		{"listen 80;", false},
		{"", false},
		{"location", false},
	}
	for _, tt := range tests {
		if got := isServerKeyword(tt.input); got != tt.want {
			t.Errorf("isServerKeyword(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseNginxDump_MultiLineServerBlock(t *testing.T) {
	input := `nginx: the configuration file /usr/local/openresty/nginx/conf/nginx.conf syntax is ok
nginx: configuration file /usr/local/openresty/nginx/conf/nginx.conf test is successful
# configuration file /usr/local/openresty/nginx/conf.d/b.example.com.conf:
server
{
    listen 80;
    listen 443 ssl;
    server_name b.example.com;
    root /www/wwwroot/b.example.com;
    access_log  /www/wwwlogs/b.example.com.log;
    error_log  /www/wwwlogs/b.example.com.error.log;
    location / {
        proxy_pass http://127.0.0.1:3000;
    }
}
# configuration file /usr/local/openresty/nginx/conf.d/default.conf:
server
{
    listen 80 default_server;
    server_name _;
    root /www/wwwroot/default.com;
    access_log  /www/wwwlogs/default.com.log;
    error_log  /www/wwwlogs/default.com.error.log;
}
`
	servers := ParseNginxDump(input)
	if len(servers) != 2 {
		t.Fatalf("期望解析出 2 个 server block，实际 %d", len(servers))
	}

	s := servers[0]
	if s.SourceFile != "/usr/local/openresty/nginx/conf.d/b.example.com.conf" {
		t.Errorf("SourceFile 不正确: %s", s.SourceFile)
	}
	if len(s.Listen) != 2 || s.Listen[0] != "80" || s.Listen[1] != "443" {
		t.Errorf("Listen 期望 [80, 443]，实际 %v", s.Listen)
	}
	if len(s.ServerNames) != 1 || s.ServerNames[0] != "b.example.com" {
		t.Errorf("ServerNames 不正确: %v", s.ServerNames)
	}
	if s.RootPath != "/www/wwwroot/b.example.com" {
		t.Errorf("RootPath 不正确: %s", s.RootPath)
	}
	if s.AccessLogPath != "/www/wwwlogs/b.example.com.log" {
		t.Errorf("AccessLogPath 不正确: %s", s.AccessLogPath)
	}
	if s.ErrorLogPath != "/www/wwwlogs/b.example.com.error.log" {
		t.Errorf("ErrorLogPath 不正确: %s", s.ErrorLogPath)
	}

	s2 := servers[1]
	if s2.SourceFile != "/usr/local/openresty/nginx/conf.d/default.conf" {
		t.Errorf("SourceFile 不正确: %s", s2.SourceFile)
	}
	if len(s2.ServerNames) != 1 || s2.ServerNames[0] != "_" {
		t.Errorf("ServerNames 不正确: %v", s2.ServerNames)
	}
}

func TestParseNginxDump_IgnoresNestedLocationLogs(t *testing.T) {
	input := `# configuration file /www/server/panel/vhost/nginx/39.106.47.3.conf:
server {
    listen 80 default_server;
    server_name 39.106.47.3;
    root /www/wwwroot/39.106.47.3;
    location ~ .*(gif|jpg|jpeg|png|bmp|swf)$ {
        error_log /dev/null;
        access_log /dev/null;
    }
    location ~ .*(js|css)?$ {
        error_log /dev/null;
        access_log /dev/null;
    }
    access_log /www/wwwlogs/39.106.47.3.log;
    error_log /www/wwwlogs/39.106.47.3.error.log;
}
`

	servers := ParseNginxDump(input)
	if len(servers) != 1 {
		t.Fatalf("期望 1 个 server block，实际 %d", len(servers))
	}
	s := servers[0]
	if s.AccessLogPath != "/www/wwwlogs/39.106.47.3.log" {
		t.Fatalf("AccessLogPath 应忽略 location 内 /dev/null，实际 %s", s.AccessLogPath)
	}
	if s.ErrorLogPath != "/www/wwwlogs/39.106.47.3.error.log" {
		t.Fatalf("ErrorLogPath 应忽略 location 内 /dev/null，实际 %s", s.ErrorLogPath)
	}
}

func TestParseNginxDump_MixedFormat(t *testing.T) {
	input := `# configuration file /etc/nginx/conf.d/a.conf:
server {
    listen 80;
    server_name a.example.com;
}
# configuration file /etc/nginx/conf.d/b.conf:
server
{
    listen 80;
    server_name b.example.com;
}
`
	servers := ParseNginxDump(input)
	if len(servers) != 2 {
		t.Fatalf("混合格式应解析出 2 个 server block，实际 %d", len(servers))
	}
	if servers[0].ServerNames[0] != "a.example.com" {
		t.Errorf("第一个 server 名不正确: %v", servers[0].ServerNames)
	}
	if servers[1].ServerNames[0] != "b.example.com" {
		t.Errorf("第二个 server 名不正确: %v", servers[1].ServerNames)
	}
}

func TestParseDirective(t *testing.T) {
	tests := []struct {
		line      string
		directive string
		args      []string
	}{
		{"listen 80;", "listen", []string{"80"}},
		{"listen 443 ssl;", "listen", []string{"443", "ssl"}},
		{"server_name a.com b.com;", "server_name", []string{"a.com", "b.com"}},
		{"root /var/www;", "root", []string{"/var/www"}},
		{"access_log /var/log/nginx/a.log;", "access_log", []string{"/var/log/nginx/a.log"}},
		{"", "", nil},
	}

	for _, tt := range tests {
		d, a := parseDirective(tt.line)
		if d != tt.directive {
			t.Errorf("directive(%q) = %q, want %q", tt.line, d, tt.directive)
		}
		if len(a) != len(tt.args) {
			t.Errorf("args(%q) = %v, want %v", tt.line, a, tt.args)
			continue
		}
		for i := range a {
			if a[i] != tt.args[i] {
				t.Errorf("args[%d](%q) = %q, want %q", i, tt.line, a[i], tt.args[i])
			}
		}
	}
}
