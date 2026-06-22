package nginx

import (
	"strings"
	"testing"
)

func TestRender_StaticNormal(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_abc123",
		PrimaryDomain:    "example.com",
		ServerNames:      "example.com www.example.com",
		HTTPPort:         80,
		RootPath:         "/www/wwwroot/example.com",
		IndexFiles:       "index.html index.htm",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/example.com.conf",
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "listen 80;")
	assertContains(t, got, "server_name example.com www.example.com;")
	assertContains(t, got, "root /www/wwwroot/example.com;")
	assertContains(t, got, "index index.html index.htm;")
	assertContains(t, got, "access_log /www/wwwlogs/example.com.access.log;")
	assertContains(t, got, "error_log /www/wwwlogs/example.com.error.log;")
	assertContains(t, got, "include /opt/nxpanel/nginx/rewrite/example.com.conf;")
	assertContains(t, got, "#NXPANEL-SITE-START site_id=site_abc123")
	assertContains(t, got, "#NXPANEL-SITE-END")
	assertContains(t, got, "#NXPANEL-LISTEN-START")
	assertContains(t, got, "#NXPANEL-SERVER-NAME-START")
	assertContains(t, got, "#NXPANEL-ROOT-START")
	assertContains(t, got, "#NXPANEL-LOG-START")
	assertContains(t, got, "#NXPANEL-REWRITE-START")
	assertContains(t, got, "#NXPANEL-DOCUMENT-START")
	assertNotContains(t, got, "#NXPANEL-SSL-START")
	assertNotContains(t, got, "#NXPANEL-FORCE-HTTPS-START")
	assertNotContains(t, got, "#NXPANEL-HOTLINK-START")
	assertNotContains(t, got, "#NXPANEL-ACCESS-LIMIT-START")
	assertNotContains(t, got, "#NXPANEL-ACME-CHALLENGE-START")
	assertNotContains(t, got, "#NXPANEL-MAIN-LOCATION-START")
	assertNotContains(t, got, "#NXPANEL-EXTRA-LOCATIONS-START")
	assertNotContains(t, got, "location /")
	assertNotContains(t, got, "try_files $uri $uri/ /index.html;")
	assertNotContains(t, got, "ssl;")
	assertNotContains(t, got, "mode=template")
}

func TestRender_MissingSiteID(t *testing.T) {
	data := &RenderData{
		PrimaryDomain: "example.com",
	}
	_, err := Render(data)
	if err == nil {
		t.Fatal("期望返回错误，但成功了")
	}
}

func TestRender_MissingPrimaryDomain(t *testing.T) {
	data := &RenderData{
		SiteID: "site_123",
	}
	_, err := Render(data)
	if err == nil {
		t.Fatal("期望返回错误，但成功了")
	}
}

func TestHashContent(t *testing.T) {
	content := []byte("hello world")
	hash := HashContent(content)
	if len(hash) != 64 {
		t.Fatalf("SHA256 hex 应为 64 字符，实际 %d", len(hash))
	}
	hash2 := HashContent(content)
	if hash != hash2 {
		t.Fatal("相同输入应产生相同 hash")
	}
	hash3 := HashContent([]byte("hello world!"))
	if hash == hash3 {
		t.Fatal("不同输入应产生不同 hash")
	}
}

func TestRender_ProxyBasic(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_proxy1",
		PrimaryDomain:    "app.example.com",
		ServerNames:      "app.example.com",
		HTTPPort:         80,
		RootPath:         "/www/wwwroot/app.example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/app.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/app.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/app.example.com.conf",
		Proxies: []*ProxyData{
			{
				ID:               "proxy_1",
				Name:             "默认代理",
				Enabled:          true,
				LocationPath:     "/",
				UpstreamURL:      "http://127.0.0.1:3000",
				HostHeader:       "$host",
				WebSocketEnabled: false,
				ConnectTimeout:   60,
				SendTimeout:      60,
				ReadTimeout:      60,
			},
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "#NXPANEL-SITE-START site_id=site_proxy1")
	assertContains(t, got, "#NXPANEL-SITE-END")
	assertContains(t, got, "#NXPANEL-MAIN-LOCATION-START")
	assertContains(t, got, "location /")
	assertContains(t, got, "proxy_pass http://127.0.0.1:3000;")

	assertNotContains(t, got, "proxy_set_header Upgrade $http_upgrade;")
	assertNotContains(t, got, "proxy_set_header Connection \"upgrade\";")
	assertNotContains(t, got, "mode=template")
}

func TestRender_ProxyWithWebSocket(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_ws1",
		PrimaryDomain:    "ws.example.com",
		ServerNames:      "ws.example.com",
		HTTPPort:         80,
		RootPath:         "/www/wwwroot/ws.example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: false,
		AccessLogPath:    "/www/wwwlogs/ws.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/ws.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/ws.example.com.conf",
		Proxies: []*ProxyData{
			{
				ID:               "proxy_2",
				Name:             "WebSocket 代理",
				Enabled:          true,
				LocationPath:     "/",
				UpstreamURL:      "http://127.0.0.1:8080",
				HostHeader:       "$http_host",
				WebSocketEnabled: true,
				ConnectTimeout:   30,
				SendTimeout:      30,
				ReadTimeout:      30,
			},
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "access_log off;")
}

func TestRender_ProxyDisabledFallsBackToStatic(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_noproxy",
		PrimaryDomain:    "static.example.com",
		ServerNames:      "static.example.com",
		HTTPPort:         80,
		RootPath:         "/www/wwwroot/static.example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/static.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/static.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/static.example.com.conf",
		Proxies: []*ProxyData{
			{
				ID:           "proxy_3",
				Name:         "禁用代理",
				Enabled:      false,
				LocationPath: "/",
				UpstreamURL:  "http://127.0.0.1:3000",
			},
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "root /www/wwwroot/static.example.com;")
	assertNotContains(t, got, "proxy_pass")
	assertNotContains(t, got, "location /")
}

func TestRender_ProxyNilFallsBackToStatic(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_noproxy2",
		PrimaryDomain:    "static2.example.com",
		ServerNames:      "static2.example.com",
		HTTPPort:         80,
		RootPath:         "/www/wwwroot/static2.example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/static2.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/static2.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/static2.example.com.conf",
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertNotContains(t, got, "proxy_pass")
	assertNotContains(t, got, "location /")
}

func TestRender_SSLForceHTTPS(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_ssl1",
		PrimaryDomain:    "secure.example.com",
		ServerNames:      "secure.example.com www.secure.example.com",
		HTTPPort:         80,
		HTTPSPort:        443,
		RootPath:         "/www/wwwroot/secure.example.com",
		IndexFiles:       "index.html index.htm",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/secure.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/secure.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/secure.example.com.conf",
		SSL: &SSLData{
			Enabled:    true,
			CertPath:   "/opt/nxpanel/nginx/ssl/site_ssl1/fullchain.pem",
			KeyPath:    "/opt/nxpanel/nginx/ssl/site_ssl1/privkey.pem",
			ForceHTTPS: true,
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "listen 80;")
	assertContains(t, got, "listen 443 ssl;")
	assertContains(t, got, "ssl_certificate /opt/nxpanel/nginx/ssl/site_ssl1/fullchain.pem;")
	assertContains(t, got, "ssl_certificate_key /opt/nxpanel/nginx/ssl/site_ssl1/privkey.pem;")
	assertContains(t, got, "ssl_protocols TLSv1.2 TLSv1.3;")
	assertContains(t, got, "ssl_ciphers EECDH+CHACHA20:")
	assertContains(t, got, "ssl_prefer_server_ciphers on;")
	assertContains(t, got, "ssl_session_cache shared:NXPANELSSL:10m;")
	assertContains(t, got, "ssl_session_timeout 10m;")
	assertContains(t, got, "Strict-Transport-Security")
	assertContains(t, got, "error_page 497")
	assertContains(t, got, "if ($scheme = http) {")
	assertContains(t, got, "return 301 https://$host$request_uri;")
	assertContains(t, got, "root /www/wwwroot/secure.example.com;")
	assertContains(t, got, "#NXPANEL-SSL-START")
	assertContains(t, got, "#NXPANEL-FORCE-HTTPS-START")
	assertNotContains(t, got, "mode=template")
}

func TestRender_SSLDualStack(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_ssl2",
		PrimaryDomain:    "dual.example.com",
		ServerNames:      "dual.example.com",
		HTTPPort:         80,
		HTTPSPort:        443,
		RootPath:         "/www/wwwroot/dual.example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: false,
		AccessLogPath:    "/www/wwwlogs/dual.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/dual.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/dual.example.com.conf",
		SSL: &SSLData{
			Enabled:    true,
			CertPath:   "/etc/ssl/dual.example.com/fullchain.pem",
			KeyPath:    "/etc/ssl/dual.example.com/privkey.pem",
			ForceHTTPS: false,
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "listen 80;")
	assertContains(t, got, "listen 443 ssl;")
	assertContains(t, got, "ssl_protocols TLSv1.2 TLSv1.3;")
	assertContains(t, got, "Strict-Transport-Security")
	assertNotContains(t, got, "return 301")
	assertContains(t, got, "access_log off;")
}

func TestRender_SSLWithProxy(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_sslproxy",
		PrimaryDomain:    "app.example.com",
		ServerNames:      "app.example.com",
		HTTPPort:         80,
		HTTPSPort:        443,
		RootPath:         "/www/wwwroot/app.example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/app.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/app.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/app.example.com.conf",
		Proxies: []*ProxyData{
			{
				ID:               "proxy_4",
				Name:             "SSL 代理",
				Enabled:          true,
				LocationPath:     "/",
				UpstreamURL:      "http://127.0.0.1:3000",
				HostHeader:       "$host",
				WebSocketEnabled: true,
				ConnectTimeout:   60,
				SendTimeout:      60,
				ReadTimeout:      60,
			},
		},
		SSL: &SSLData{
			Enabled:    true,
			CertPath:   "/opt/nxpanel/nginx/ssl/site_sslproxy/fullchain.pem",
			KeyPath:    "/opt/nxpanel/nginx/ssl/site_sslproxy/privkey.pem",
			ForceHTTPS: true,
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "if ($scheme = http) {")
	assertContains(t, got, "return 301 https://$host$request_uri;")
	assertContains(t, got, "listen 443 ssl;")
	assertContains(t, got, "ssl_protocols TLSv1.2 TLSv1.3;")
	assertContains(t, got, "Strict-Transport-Security")
}

func TestRender_NoSSLNoForceHTTPS(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_nossl",
		PrimaryDomain:    "nossl.example.com",
		ServerNames:      "nossl.example.com",
		HTTPPort:         80,
		RootPath:         "/www/wwwroot/nossl.example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/nossl.example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/nossl.example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/nossl.example.com.conf",
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertContains(t, got, "listen 80;")
	assertNotContains(t, got, "listen 443 ssl;")
	assertNotContains(t, got, "ssl_certificate")
	assertNotContains(t, got, "ssl_protocols")
	assertNotContains(t, got, "ssl_ciphers")
	assertNotContains(t, got, "Strict-Transport-Security")
	assertNotContains(t, got, "error_page 497")
	assertNotContains(t, got, "return 301")
	assertNotContains(t, got, "#NXPANEL-SSL-START")
	assertNotContains(t, got, "#NXPANEL-SSL-END")
	assertNotContains(t, got, "#NXPANEL-FORCE-HTTPS-START")
	assertNotContains(t, got, "#NXPANEL-FORCE-HTTPS-END")
}

func TestBuildDocumentBlock_AutoindexOptions(t *testing.T) {
	got := BuildDocumentBlock(DocumentData{
		AutoindexEnabled:   true,
		AutoindexExactSize: false,
		AutoindexLocaltime: true,
		AutoindexFormat:    "json",
		ErrorPage404:       "/404.html",
	})

	assertContains(t, got, "autoindex on;")
	assertContains(t, got, "autoindex_exact_size off;")
	assertContains(t, got, "autoindex_localtime on;")
	assertContains(t, got, "autoindex_format json;")
	assertContains(t, got, "error_page 404 /404.html;")
}

func TestBuildDocumentBlock_NoAutoindexSkipsOptions(t *testing.T) {
	got := BuildDocumentBlock(DocumentData{
		AutoindexEnabled:   false,
		AutoindexExactSize: true,
		AutoindexLocaltime: true,
		AutoindexFormat:    "json",
	})

	assertContains(t, got, "autoindex off;")
	assertNotContains(t, got, "autoindex_exact_size")
	assertNotContains(t, got, "autoindex_localtime")
	assertNotContains(t, got, "autoindex_format")
}

func TestRender_MultipleBindingsSingleBlock(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_multi",
		PrimaryDomain:    "example.com",
		ServerNames:      "example.com",
		HTTPPort:         80,
		HTTPSPort:        443,
		RootPath:         "/www/wwwroot/example.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/example.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/example.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/example.com.conf",
		Bindings: []Binding{
			{Domain: "example.com", Port: 80},
			{Domain: "blog.com", Port: 80},
			{Domain: "app.com", Port: 8080},
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertCount(t, got, "#NXPANEL-SITE-START", 1)
	assertContains(t, got, "server_name example.com blog.com app.com;")
	assertContains(t, got, "listen 80;")
	assertContains(t, got, "listen 8080;")
	assertNotContains(t, got, "listen 443 ssl;")
}

func TestRender_MultipleBindingsWithSSL(t *testing.T) {
	data := &RenderData{
		SiteID:           "site_ssl_multi",
		PrimaryDomain:    "secure.com",
		ServerNames:      "secure.com",
		HTTPPort:         80,
		HTTPSPort:        443,
		RootPath:         "/www/wwwroot/secure.com",
		IndexFiles:       "index.html",
		AccessLogEnabled: true,
		AccessLogPath:    "/www/wwwlogs/secure.com.access.log",
		ErrorLogPath:     "/www/wwwlogs/secure.com.error.log",
		RewritePath:      "/opt/nxpanel/nginx/rewrite/secure.com.conf",
		Bindings: []Binding{
			{Domain: "secure.com", Port: 80},
			{Domain: "www.secure.com", Port: 80},
		},
		SSL: &SSLData{
			Enabled:    true,
			CertPath:   "/opt/nxpanel/nginx/ssl/site_ssl_multi/fullchain.pem",
			KeyPath:    "/opt/nxpanel/nginx/ssl/site_ssl_multi/privkey.pem",
			ForceHTTPS: true,
		},
	}

	got, err := Render(data)
	if err != nil {
		t.Fatalf("Render 失败: %v", err)
	}

	assertCount(t, got, "#NXPANEL-SITE-START", 1)
	assertContains(t, got, "server_name secure.com www.secure.com;")
	assertContains(t, got, "listen 80;")
	assertContains(t, got, "listen 443 ssl;")
	assertContains(t, got, "ssl_certificate /opt/nxpanel/nginx/ssl/site_ssl_multi/fullchain.pem;")
	assertContains(t, got, "ssl_protocols TLSv1.2 TLSv1.3;")
	assertContains(t, got, "Strict-Transport-Security")
	assertContains(t, got, "error_page 497")
	assertContains(t, got, "return 301 https://$host$request_uri;")
}

func assertCount(t *testing.T, haystack, needle string, expected int) {
	t.Helper()
	count := strings.Count(haystack, needle)
	if count != expected {
		t.Errorf("期望出现 %d 次 %q，实际 %d 次\n输出:\n%s", expected, needle, count, haystack)
	}
}

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("期望包含 %q，实际输出:\n%s", needle, haystack)
	}
}

func assertNotContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("期望不包含 %q，实际输出:\n%s", needle, haystack)
	}
}
