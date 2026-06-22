// security 包测试 — 路径安全检查和文本安全检查
package security

import (
	"os"
	"path/filepath"
	"testing"
)

// ============================================================
// 路径安全测试
// ============================================================

func TestCleanAbsWithin_ValidPath(t *testing.T) {
	tmpDir := t.TempDir()
	panelRoot := filepath.Join(tmpDir, "opt", "nxpanel", "nginx")
	webRoot := filepath.Join(tmpDir, "www", "wwwroot")
	if err := os.MkdirAll(panelRoot, 0755); err != nil {
		t.Fatalf("创建面板测试根目录失败: %v", err)
	}
	if err := os.MkdirAll(webRoot, 0755); err != nil {
		t.Fatalf("创建网站测试根目录失败: %v", err)
	}
	roots := []string{panelRoot, webRoot}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"面板配置文件", filepath.Join(panelRoot, "sites-available", "test.conf"), filepath.Join(panelRoot, "sites-available", "test.conf")},
		{"网站根目录", filepath.Join(webRoot, "example.com", "index.html"), filepath.Join(webRoot, "example.com", "index.html")},
		{"根目录本身", panelRoot, panelRoot},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CleanAbsWithin(tt.path, roots)
			if err != nil {
				t.Errorf("期望成功，实际错误: %v", err)
			}
			if got != tt.want {
				t.Errorf("期望 %s，实际 %s", tt.want, got)
			}
		})
	}
}

func TestCleanAbsWithin_InvalidPath(t *testing.T) {
	roots := []string{"/opt/nxpanel/nginx", "/www/wwwroot"}

	tests := []struct {
		name string
		path string
	}{
		{"空路径", ""},
		{"包含空字节", "/opt/test\x00.conf"},
		{"包含换行", "/opt/test\n.conf"},
		{"包含回车", "/opt/test\r.conf"},
		{"相对路径", "relative/path"},
		{"路径穿越", "/opt/nxpanel/nginx/../../../etc/passwd"},
		{"不在允许范围内", "/etc/passwd"},
		{"不在允许范围内2", "/tmp/test.conf"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CleanAbsWithin(tt.path, roots)
			if err == nil {
				t.Error("期望返回错误，实际成功")
			}
		})
	}
}

func TestCleanAbsWithin_CleansPath(t *testing.T) {
	roots := []string{"/opt/nxpanel/nginx"}

	// 双斜杠应被清理
	got, err := CleanAbsWithin("/opt/nxpanel/nginx//sites-available/test.conf", roots)
	if err != nil {
		t.Errorf("期望成功，实际错误: %v", err)
	}
	if got != "/opt/nxpanel/nginx/sites-available/test.conf" {
		t.Errorf("路径未被清理: %s", got)
	}

	// 末尾的点应被清理
	got2, err := CleanAbsWithin("/opt/nxpanel/nginx/sites-available/./test.conf", roots)
	if err != nil {
		t.Errorf("期望成功，实际错误: %v", err)
	}
	if got2 != "/opt/nxpanel/nginx/sites-available/test.conf" {
		t.Errorf("路径未被清理: %s", got2)
	}
}

func TestCleanAbsWithin_NewFileUnderSymlinkParent(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	insideDir := filepath.Join(allowedDir, "inside")
	outsideDir := filepath.Join(tmpDir, "outside")

	if err := os.MkdirAll(insideDir, 0755); err != nil {
		t.Fatalf("创建白名单内目录失败: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("创建白名单外目录失败: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(allowedDir, "link-out")); err != nil {
		t.Fatalf("创建指向白名单外的 symlink 失败: %v", err)
	}
	if err := os.Symlink(insideDir, filepath.Join(allowedDir, "link-in")); err != nil {
		t.Fatalf("创建指向白名单内的 symlink 失败: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "正常新文件仍允许",
			path:    filepath.Join(allowedDir, "new.conf"),
			wantErr: false,
		},
		{
			name:    "多级新目录仍允许",
			path:    filepath.Join(allowedDir, "new-dir", "site", "new.conf"),
			wantErr: false,
		},
		{
			name:    "父目录 symlink 指向白名单外时拒绝新文件",
			path:    filepath.Join(allowedDir, "link-out", "new.conf"),
			wantErr: true,
		},
		{
			name:    "父目录 symlink 指向白名单内时允许",
			path:    filepath.Join(allowedDir, "link-in", "new.conf"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CleanAbsWithin(tt.path, []string{allowedDir})
			if (err != nil) != tt.wantErr {
				t.Fatalf("CleanAbsWithin() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestIsPathSafe(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/test.conf", true},
		{"", false},
		{"/opt/test\x00.conf", false},
		{"/opt/test\n.conf", false},
		{"/opt/test\r.conf", false},
	}

	for _, tt := range tests {
		got := IsPathSafe(tt.path)
		if got != tt.want {
			t.Errorf("IsPathSafe(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// ============================================================
// 文本安全测试
// ============================================================

func TestValidateText(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		maxLen int
		want   bool
	}{
		{"合法文本", "hello", 100, true},
		{"空字节", "hello\x00world", 100, false},
		{"换行", "hello\nworld", 100, false},
		{"回车", "hello\rworld", 100, false},
		{"超长", "hello", 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateText(tt.text, tt.maxLen)
			if (err == nil) != tt.want {
				t.Errorf("ValidateText(%q, %d) err=%v, want success=%v", tt.text, tt.maxLen, err, tt.want)
			}
		})
	}
}

func TestValidateContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		maxLen  int
		want    bool
	}{
		{"合法内容（含换行）", "server {\n    listen 80;\n}", 1024, true},
		{"空字节", "server\x00{}", 1024, false},
		{"超长", "abc", 2, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateContent(tt.content, tt.maxLen)
			if (err == nil) != tt.want {
				t.Errorf("ValidateContent(%q, %d) err=%v, want success=%v", tt.content, tt.maxLen, err, tt.want)
			}
		})
	}
}
