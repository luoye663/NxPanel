// nginx 包测试 — detector 和 inspector 的纯函数测试
package nginx

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================
// detector.go 测试
// ============================================================

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    string
	}{
		{
			name:   "标准 nginx -V 输出",
			output: "nginx version: nginx/1.24.0\nconfigure arguments: --prefix=/etc/nginx\n",
			want:   "nginx/1.24.0",
		},
		{
			name:   "OpenResty 版本",
			output: "nginx version: openresty/1.25.3.1\nconfigure arguments: ...\n",
			want:   "openresty/1.25.3.1",
		},
		{
			name:   "空输出",
			output: "",
			want:   "",
		},
		{
			name:   "没有 version 行",
			output: "configure arguments: --prefix=/etc/nginx\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseVersion(tt.output)
			if got != tt.want {
				t.Errorf("ParseVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseConfigurePath(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name: "标准输出",
			output: "nginx version: nginx/1.24.0\n" +
				"configure arguments: --prefix=/etc/nginx --conf-path=/etc/nginx/nginx.conf --sbin-path=/usr/sbin/nginx\n",
			want: "/etc/nginx/nginx.conf",
		},
		{
			name: "OpenResty -V 输出",
			output: "nginx version: openresty/1.25.3.1\n" +
				"configure arguments: --prefix=/usr/local/openresty/nginx --conf-path=/usr/local/openresty/nginx/conf/nginx.conf\n",
			want: "/usr/local/openresty/nginx/conf/nginx.conf",
		},
		{
			name:   "没有 conf-path",
			output: "configure arguments: --prefix=/etc/nginx\n",
			want:   "",
		},
		{
			name:   "空输出",
			output: "",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseConfigurePath(tt.output)
			if got != tt.want {
				t.Errorf("ParseConfigurePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseConfPathFromTestOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "nginx -t 成功输出",
			output: "nginx: the configuration file /etc/nginx/nginx.conf syntax is ok\nnginx: configuration file /etc/nginx/nginx.conf test is successful\n",
			want:   "/etc/nginx/nginx.conf",
		},
		{
			name:   "openresty -t 成功输出",
			output: "nginx: the configuration file /usr/local/openresty/nginx/conf/nginx.conf syntax is ok\nnginx: configuration file /usr/local/openresty/nginx/conf/nginx.conf test is successful\n",
			want:   "/usr/local/openresty/nginx/conf/nginx.conf",
		},
		{
			name:   "只有 syntax is ok",
			output: "nginx: the configuration file /usr/local/openresty/nginx/conf/nginx.conf syntax is ok\n",
			want:   "/usr/local/openresty/nginx/conf/nginx.conf",
		},
		{
			name:   "只有 test is successful",
			output: "nginx: configuration file /etc/nginx/nginx.conf test is successful\n",
			want:   "/etc/nginx/nginx.conf",
		},
		{
			name:   "空输出",
			output: "",
			want:   "",
		},
		{
			name:   "错误输出无路径",
			output: "nginx: [emerg] unexpected end of file\n",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseConfPathFromTestOutput(tt.output)
			if got != tt.want {
				t.Errorf("ParseConfPathFromTestOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParsePrefix(t *testing.T) {
	output := "nginx version: nginx/1.24.0\n" +
		"configure arguments: --prefix=/etc/nginx --conf-path=/etc/nginx/nginx.conf\n"
	got := ParsePrefix(output)
	if got != "/etc/nginx" {
		t.Errorf("ParsePrefix() = %q, want %q", got, "/etc/nginx")
	}
}

// ============================================================
// inspector.go 测试
// ============================================================

func TestCheckIncludeInstalled(t *testing.T) {
	t.Run("文件不存在", func(t *testing.T) {
		if CheckIncludeInstalled("/nonexistent/path") {
			t.Error("不存在的文件应返回 false")
		}
	})

	t.Run("文件存在且包含标记", func(t *testing.T) {
		tmpDir := t.TempDir()
		f := filepath.Join(tmpDir, "nxpanel.conf")
		content := MarkerIncludeStart + "\ninclude /opt/nxpanel/nginx/conf.d/*.conf;\ninclude /opt/nxpanel/nginx/sites-enabled/*.conf;\n" + MarkerIncludeEnd + "\n"
		os.WriteFile(f, []byte(content), 0644)

		if !CheckIncludeInstalled(f) {
			t.Error("包含标记的文件应返回 true")
		}
	})

	t.Run("文件存在但不包含标记", func(t *testing.T) {
		tmpDir := t.TempDir()
		f := filepath.Join(tmpDir, "other.conf")
		os.WriteFile(f, []byte("server { listen 80; }"), 0644)

		if CheckIncludeInstalled(f) {
			t.Error("不包含标记的文件应返回 false")
		}
	})
}

func TestInsertIncludeInHTTPBlock(t *testing.T) {
	t.Run("简单 http 块", func(t *testing.T) {
		content := "http {\n  server {\n    listen 80;\n  }\n}\n"
		result, err := InsertIncludeInHTTPBlock(content, "/opt/nxpanel/nginx")
		if err != nil {
			t.Fatalf("InsertIncludeInHTTPBlock 失败: %v", err)
		}
		if !contains(result, MarkerIncludeStart) {
			t.Error("结果应包含 include 开始标记")
		}
		if !contains(result, MarkerIncludeEnd) {
			t.Error("结果应包含 include 结束标记")
		}
		if !contains(result, "sites-enabled") {
			t.Error("结果应包含 sites-enabled 路径")
		}
	})

	t.Run("没有 http 块", func(t *testing.T) {
		content := "events {\n  worker_connections 1024;\n}\n"
		_, err := InsertIncludeInHTTPBlock(content, "/opt/nxpanel/nginx")
		if err == nil {
			t.Error("没有 http 块应返回错误")
		}
	})

	t.Run("带注释的 http 块", func(t *testing.T) {
		content := "# 这是注释\nhttp {\n  # 另一个注释\n  server {\n    listen 80;\n  }\n}\n"
		result, err := InsertIncludeInHTTPBlock(content, "/opt/nxpanel/nginx")
		if err != nil {
			t.Fatalf("InsertIncludeInHTTPBlock 失败: %v", err)
		}
		if !contains(result, MarkerIncludeStart) {
			t.Error("结果应包含 include 标记")
		}
	})

	t.Run("嵌套大括号", func(t *testing.T) {
		content := "http {\n  server {\n    location / {\n      proxy_pass http://backend;\n    }\n  }\n}\n"
		result, err := InsertIncludeInHTTPBlock(content, "/opt/nxpanel/nginx")
		if err != nil {
			t.Fatalf("InsertIncludeInHTTPBlock 失败: %v", err)
		}
		if !contains(result, MarkerIncludeStart) {
			t.Error("结果应包含 include 标记")
		}
	})
}

func TestParseWebUser(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantUser string
		wantGrp  string
	}{
		{
			name: "标准 user 指令：user www www;",
			content: `user www www;
worker_processes auto;
events { worker_connections 1024; }
http { server { listen 80; } }
`,
			wantUser: "www",
			wantGrp:  "www",
		},
		{
			name: "省略 group：user nginx;",
			content: `user nginx;
events { worker_connections 1024; }
http { server { listen 80; } }
`,
			wantUser: "nginx",
			wantGrp:  "nginx",
		},
		{
			name: "忽略 http 块内的 user",
			content: `user www;
http {
    server { listen 80; }
}`,
			wantUser: "www",
			wantGrp:  "www",
		},
		{
			name:    "没有 user 指令",
			content: `events { worker_connections 1024; }
http { server { listen 80; } }
`,
			wantUser: "",
			wantGrp:  "",
		},
		{
			name:     "空文件",
			content:  "",
			wantUser: "",
			wantGrp:  "",
		},
		{
			name: "注释行不干扰",
			content: `# user commented;
user www-data;
events { }
http { }
`,
			wantUser: "www-data",
			wantGrp:  "www-data",
		},
		{
			name: "带额外空格",
			content: `user   www   www;
events { }
http { }
`,
			wantUser: "www",
			wantGrp:  "www",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUser, gotGrp := ParseWebUser(tt.content)
			if gotUser != tt.wantUser {
				t.Errorf("ParseWebUser() user = %q, want %q", gotUser, tt.wantUser)
			}
			if gotGrp != tt.wantGrp {
				t.Errorf("ParseWebUser() group = %q, want %q", gotGrp, tt.wantGrp)
			}
		})
	}
}

func TestEnsurePanelDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	panelDir := filepath.Join(tmpDir, "nginx")

	if err := EnsurePanelDirectories(panelDir); err != nil {
		t.Fatalf("EnsurePanelDirectories 失败: %v", err)
	}

	expectedDirs := []string{
		"conf.d",
		"sites-available",
		"sites-enabled",
		"rewrite",
		"ssl",
		"backups",
	}

	for _, dir := range expectedDirs {
		p := filepath.Join(panelDir, dir)
		info, err := os.Stat(p)
		if err != nil {
			t.Errorf("目录 %s 应存在: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s 应该是目录", dir)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
