// agent 包测试 — token 认证、路径策略、原子写入、文件事务
package agent

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
)

// ============================================================
// Token 认证中间件测试
// ============================================================

func TestTokenAuth_ValidToken(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := TokenAuth("my-secret-token")(next)

	req := httptest.NewRequest("GET", "/internal/v1/health", nil)
	req.Header.Set("X-NxPanel-Agent-Token", "my-secret-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("正确的 token 应放行")
	}
}

func TestTokenAuth_InvalidToken(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := TokenAuth("my-secret-token")(next)

	req := httptest.NewRequest("GET", "/internal/v1/health", nil)
	req.Header.Set("X-NxPanel-Agent-Token", "wrong-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("错误的 token 不应放行")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("状态码期望 401，实际 %d", rec.Code)
	}
}

func TestTokenAuth_MissingToken(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := TokenAuth("my-secret-token")(next)

	req := httptest.NewRequest("GET", "/internal/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if called {
		t.Error("缺少 token 不应放行")
	}
}

func TestTokenAuth_EmptyTokenSkipsAuth(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	// 空字符串 token 应跳过认证（开发模式）
	handler := TokenAuth("")(next)

	req := httptest.NewRequest("GET", "/internal/v1/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("空 token 模式应跳过认证并放行")
	}
}

// ============================================================
// 路径策略测试
// ============================================================

func TestPathPolicy_ValidPaths(t *testing.T) {
	tmpDir := t.TempDir()
	panelRoot := filepath.Join(tmpDir, "panel-nginx")
	webRoot := filepath.Join(tmpDir, "wwwroot")
	if err := os.MkdirAll(panelRoot, 0755); err != nil {
		t.Fatalf("创建面板测试根目录失败: %v", err)
	}
	if err := os.MkdirAll(webRoot, 0755); err != nil {
		t.Fatalf("创建网站测试根目录失败: %v", err)
	}
	policy := NewPathPolicy([]string{panelRoot, webRoot})

	tests := []struct {
		name string
		path string
	}{
		{"面板配置", filepath.Join(panelRoot, "sites-available", "test.conf")},
		{"网站根目录", filepath.Join(webRoot, "example.com")},
		{"备份目录", filepath.Join(panelRoot, "backups", "op_test", "file.conf")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := policy.Validate(tt.path)
			if err != nil {
				t.Errorf("路径 %s 应通过验证: %v", tt.path, err)
			}
		})
	}
}

func TestPathPolicy_InvalidPaths(t *testing.T) {
	policy := NewDefaultPathPolicy()

	tests := []struct {
		name string
		path string
	}{
		{"系统路径", "/etc/passwd"},
		{"临时目录", "/tmp/evil.conf"},
		{"路径穿越", "/opt/nxpanel/nginx/../../../etc/passwd"},
		{"空字节", "/opt/test\x00.conf"},
		{"相对路径", "relative/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := policy.Validate(tt.path)
			if err == nil {
				t.Errorf("路径 %s 不应通过验证", tt.path)
			}
		})
	}
}

func TestHandleFilesReadRejectsLargeFile(t *testing.T) {
	allowedDir := t.TempDir()
	path := filepath.Join(allowedDir, "large.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	if err := f.Truncate(2*1024*1024 + 1); err != nil {
		f.Close()
		t.Fatalf("扩展测试文件失败: %v", err)
	}
	f.Close()

	cfg := app.DefaultConfig()
	cfg.Agent.MaxReadSize = "2M"
	server := &Server{cfg: cfg, policy: NewPathPolicy([]string{allowedDir})}
	req := httptest.NewRequest("POST", "/internal/v1/files/read", strings.NewReader(fmt.Sprintf(`{"path":%q}`, path)))
	rec := httptest.NewRecorder()

	server.handleFilesRead(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("大文件读取应返回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleFilesDownloadRejectsLargeFile(t *testing.T) {
	allowedDir := t.TempDir()
	path := filepath.Join(allowedDir, "large.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("创建测试文件失败: %v", err)
	}
	if err := f.Truncate(2*1024*1024 + 1); err != nil {
		f.Close()
		t.Fatalf("扩展测试文件失败: %v", err)
	}
	f.Close()

	cfg := app.DefaultConfig()
	cfg.Agent.MaxDownloadSize = "2M"
	server := &Server{cfg: cfg, policy: NewPathPolicy([]string{allowedDir})}
	req := httptest.NewRequest("GET", "/internal/v1/files/download?path="+path, nil)
	rec := httptest.NewRecorder()

	server.handleFilesDownload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("大文件下载应返回 400，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
}

func TestPathPolicy_FromConfig(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := app.DefaultConfig()
	cfg.DataDir = filepath.Join(tmpDir, "data")
	cfg.Nginx.PanelDir = filepath.Join(tmpDir, "panel-nginx")
	cfg.Nginx.ConfPath = filepath.Join(tmpDir, "openresty", "conf", "nginx.conf")
	cfg.Nginx.LogDir = filepath.Join(tmpDir, "logs")
	cfg.Nginx.AllowedRootPrefixes = []string{filepath.Join(tmpDir, "sites")}
	cfg.Nginx.AllowedLogPrefixes = []string{filepath.Join(tmpDir, "custom-logs")}
	cfg.Agent.AllowedRoots = []string{filepath.Join(tmpDir, "extra")}

	for _, dir := range []string{
		cfg.DataDir,
		cfg.Nginx.PanelDir,
		filepath.Dir(cfg.Nginx.ConfPath),
		cfg.Nginx.LogDir,
		cfg.Nginx.AllowedRootPrefixes[0],
		cfg.Nginx.AllowedLogPrefixes[0],
		cfg.Agent.AllowedRoots[0],
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("创建测试目录失败: %v", err)
		}
	}

	policy := NewPathPolicyFromConfig(cfg)
	tests := []string{
		filepath.Join(cfg.DataDir, "panel.db"),
		filepath.Join(cfg.Nginx.PanelDir, "sites-available", "test.conf"),
		cfg.Nginx.ConfPath,
		filepath.Join(cfg.Nginx.LogDir, "site.access.log"),
		filepath.Join(cfg.Nginx.AllowedRootPrefixes[0], "example.com"),
		filepath.Join(cfg.Nginx.AllowedLogPrefixes[0], "site.error.log"),
		filepath.Join(cfg.Agent.AllowedRoots[0], "file.txt"),
	}

	for _, path := range tests {
		if _, err := policy.Validate(path); err != nil {
			t.Errorf("路径 %s 应通过验证: %v", path, err)
		}
	}
}

func TestPathPolicy_DefaultDoesNotAllowBroadSystemRoots(t *testing.T) {
	policy := NewDefaultPathPolicy()

	for _, path := range []string{
		"/www/wwwlogs/test.access.log",
		"/var/log/nginx/site.access.log",
		"/etc/nginx/nginx.conf",
		"/usr/share/nginx/html/index.html",
	} {
		if _, err := policy.Validate(path); err == nil {
			t.Errorf("路径 %s 不应被默认白名单允许", path)
		}
	}
}

// ============================================================
// 原子写入测试
// ============================================================

func TestWriteFileAtomic(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "test.conf")
	content := []byte("server {\n    listen 80;\n}")

	if err := writeFileAtomic(targetPath, content, 0644); err != nil {
		t.Fatalf("原子写入失败: %v", err)
	}

	// 验证文件内容
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("文件内容不匹配，期望 %q，实际 %q", string(content), string(data))
	}

	// 验证文件权限
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("获取文件信息失败: %v", err)
	}
	if info.Mode().Perm() != 0644 {
		t.Errorf("文件权限期望 0644，实际 %o", info.Mode().Perm())
	}
}

func TestWriteFileAtomic_OverwriteExisting(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "test.conf")

	// 先创建初始文件
	if err := writeFileAtomic(targetPath, []byte("old content"), 0644); err != nil {
		t.Fatalf("初始写入失败: %v", err)
	}

	// 覆盖写入
	if err := writeFileAtomic(targetPath, []byte("new content"), 0644); err != nil {
		t.Fatalf("覆盖写入失败: %v", err)
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("文件内容不匹配，期望 'new content'，实际 %q", string(data))
	}
}

func TestWriteFileAtomic_CreatesDeepPath(t *testing.T) {
	// 注意：writeFileAtomic 不创建父目录，由 transaction.applyOne 负责创建
	// 所以这里只测试父目录已存在的情况
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "test.conf")

	if err := writeFileAtomic(targetPath, []byte("test"), 0644); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
}

// ============================================================
// 文件事务测试
// ============================================================

func TestTransaction_WriteAndRollback(t *testing.T) {
	tmpDir := t.TempDir()
	// 创建允许的根目录
	allowedDir := filepath.Join(tmpDir, "nginx")
	os.MkdirAll(allowedDir, 0755)

	// 创建已有文件
	existingFile := filepath.Join(allowedDir, "existing.conf")
	os.WriteFile(existingFile, []byte("old content"), 0644)

	// 创建路径策略（使用临时目录）
	policy := NewPathPolicy([]string{allowedDir})
	backupDir := filepath.Join(tmpDir, "backups")

	tx := NewTransaction("op_test_001", backupDir, policy, "", "")

	// 写入新文件并修改已有文件
	newFile := filepath.Join(allowedDir, "new.conf")
	changes := []FileChange{
		{Type: "write", Path: newFile, Content: []byte("new content"), Perm: 0644},
		{Type: "write", Path: existingFile, Content: []byte("updated content"), Perm: 0644},
	}

	ctx := context.Background()
	if err := tx.Apply(ctx, changes); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

	// 验证文件已写入
	data, _ := os.ReadFile(newFile)
	if string(data) != "new content" {
		t.Errorf("新文件内容不匹配: %q", string(data))
	}
	data, _ = os.ReadFile(existingFile)
	if string(data) != "updated content" {
		t.Errorf("更新文件内容不匹配: %q", string(data))
	}

	// 验证备份存在
	if len(tx.Backups) != 2 {
		t.Fatalf("期望 2 个备份，实际 %d", len(tx.Backups))
	}

	// 回滚
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback 失败: %v", err)
	}

	// 验证新文件已删除
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Error("回滚后新文件应被删除")
	}

	// 验证已有文件已恢复
	data, _ = os.ReadFile(existingFile)
	if string(data) != "old content" {
		t.Errorf("回滚后文件内容应为 'old content'，实际 %q", string(data))
	}
}

func TestTransaction_SymlinkAndRemove(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "nginx")
	sitesAvail := filepath.Join(allowedDir, "sites-available")
	sitesEnabled := filepath.Join(allowedDir, "sites-enabled")
	os.MkdirAll(sitesAvail, 0755)
	os.MkdirAll(sitesEnabled, 0755)

	// 创建源文件
	sourceFile := filepath.Join(sitesAvail, "test.conf")
	os.WriteFile(sourceFile, []byte("config"), 0644)

	policy := NewPathPolicy([]string{allowedDir})
	backupDir := filepath.Join(tmpDir, "backups")

	tx := NewTransaction("op_test_002", backupDir, policy, "", "")

	// 创建符号链接
	linkPath := filepath.Join(sitesEnabled, "test.conf")
	changes := []FileChange{
		{Type: "symlink", Path: linkPath, Target: "../sites-available/test.conf"},
	}

	ctx := context.Background()
	if err := tx.Apply(ctx, changes); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

	// 验证符号链接
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("读取符号链接失败: %v", err)
	}
	if target != "../sites-available/test.conf" {
		t.Errorf("符号链接目标不匹配: %s", target)
	}
}

func TestTransaction_RejectsInvalidPath(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "nginx")
	os.MkdirAll(allowedDir, 0755)

	policy := NewPathPolicy([]string{allowedDir})
	backupDir := filepath.Join(tmpDir, "backups")

	tx := NewTransaction("op_test_003", backupDir, policy, "", "")

	// 尝试写入不允许的路径
	changes := []FileChange{
		{Type: "write", Path: "/etc/passwd", Content: []byte("evil"), Perm: 0644},
	}

	ctx := context.Background()
	err := tx.Apply(ctx, changes)
	if err == nil {
		t.Fatal("非法路径应被拒绝")
	}
}

func TestTransaction_RejectsWriteUnderSymlinkParentOutsideAllowedRoot(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "nginx")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(allowedDir, 0755); err != nil {
		t.Fatalf("创建允许目录失败: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("创建外部目录失败: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(allowedDir, "link-out")); err != nil {
		t.Fatalf("创建 symlink 失败: %v", err)
	}

	policy := NewPathPolicy([]string{allowedDir})
	tx := NewTransaction("op_test_symlink_parent", filepath.Join(tmpDir, "backups"), policy, "", "")
	targetPath := filepath.Join(allowedDir, "link-out", "new.conf")

	err := tx.Apply(context.Background(), []FileChange{
		{Type: "write", Path: targetPath, Content: []byte("evil"), Perm: 0644},
	})
	if err == nil {
		t.Fatal("写入白名单内 symlink 指向的外部新文件应被拒绝")
	}
	if _, statErr := os.Stat(filepath.Join(outsideDir, "new.conf")); !os.IsNotExist(statErr) {
		t.Fatalf("外部目录不应被写入，stat err: %v", statErr)
	}
}

func TestTransaction_MkdirAndTruncate(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "nginx")
	os.MkdirAll(allowedDir, 0755)

	// 创建将被截断的文件
	truncFile := filepath.Join(allowedDir, "truncateme.log")
	os.WriteFile(truncFile, []byte("log content here"), 0644)

	policy := NewPathPolicy([]string{allowedDir})
	backupDir := filepath.Join(tmpDir, "backups")

	tx := NewTransaction("op_test_004", backupDir, policy, "", "")

	newDir := filepath.Join(allowedDir, "ssl", "site_001")
	changes := []FileChange{
		{Type: "mkdir", Path: newDir, Perm: 0755},
		{Type: "truncate", Path: truncFile},
	}

	ctx := context.Background()
	if err := tx.Apply(ctx, changes); err != nil {
		t.Fatalf("Apply 失败: %v", err)
	}

	// 验证目录已创建
	info, err := os.Stat(newDir)
	if err != nil {
		t.Fatalf("目录未创建: %v", err)
	}
	if !info.IsDir() {
		t.Error("期望是目录")
	}

	// 验证文件已截断
	data, _ := os.ReadFile(truncFile)
	if len(data) != 0 {
		t.Errorf("文件应被截断，实际有 %d 字节", len(data))
	}
}

func TestExtractZip_NormalFile(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	destDir := filepath.Join(allowedDir, "extract")
	archivePath := filepath.Join(allowedDir, "ok.zip")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("创建解压目录失败: %v", err)
	}
	writeTestZip(t, archivePath, map[string]string{"dir/file.txt": "ok"})

	if err := extractZip(NewPathPolicy([]string{allowedDir}), archivePath, destDir); err != nil {
		t.Fatalf("正常 zip 解压应成功: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destDir, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("读取解压文件失败: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("解压内容不匹配: %q", string(data))
	}
}

func TestExtractZip_RejectsZipSlipAbsoluteAndBackslash(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	destDir := filepath.Join(allowedDir, "extract")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("创建解压目录失败: %v", err)
	}
	policy := NewPathPolicy([]string{allowedDir})

	zipSlipPath := filepath.Join(allowedDir, "slip.zip")
	writeTestZip(t, zipSlipPath, map[string]string{"../evil.txt": "evil"})
	if err := extractZip(policy, zipSlipPath, destDir); err == nil {
		t.Fatal("zip slip 条目应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(allowedDir, "evil.txt")); !os.IsNotExist(err) {
		t.Fatalf("zip slip 不应写出解压目录，stat err: %v", err)
	}

	absolutePath := filepath.Join(allowedDir, "absolute.zip")
	writeTestZip(t, absolutePath, map[string]string{"/tmp/evil.txt": "evil"})
	if err := extractZip(policy, absolutePath, destDir); err == nil {
		t.Fatal("绝对路径 zip 条目应被拒绝")
	}

	backslashPath := filepath.Join(allowedDir, "backslash.zip")
	writeTestZip(t, backslashPath, map[string]string{"..\\evil.txt": "evil"})
	if err := extractZip(policy, backslashPath, destDir); err == nil {
		t.Fatal("包含反斜杠穿越的 zip 条目应被拒绝")
	}
}

func TestExtractZip_RejectsSymlinkParentOutsideAllowedRoot(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	destDir := filepath.Join(allowedDir, "extract")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("创建解压目录失败: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("创建外部目录失败: %v", err)
	}
	if err := os.Symlink(outsideDir, filepath.Join(destDir, "link-out")); err != nil {
		t.Fatalf("创建 symlink 失败: %v", err)
	}
	archivePath := filepath.Join(allowedDir, "symlink.zip")
	writeTestZip(t, archivePath, map[string]string{"link-out/new.txt": "evil"})

	if err := extractZip(NewPathPolicy([]string{allowedDir}), archivePath, destDir); err == nil {
		t.Fatal("写入解压目录内 symlink 指向的外部新文件应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("外部目录不应被写入，stat err: %v", err)
	}
}

func TestExtractTar_RejectsTarSlipAndSymlinkParent(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	destDir := filepath.Join(allowedDir, "extract")
	outsideDir := filepath.Join(tmpDir, "outside")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("创建解压目录失败: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("创建外部目录失败: %v", err)
	}
	policy := NewPathPolicy([]string{allowedDir})

	tarSlipPath := filepath.Join(allowedDir, "slip.tar")
	writeTestTar(t, tarSlipPath, false, map[string]string{"../evil.txt": "evil"})
	if err := extractTar(policy, tarSlipPath, destDir, false); err == nil {
		t.Fatal("tar slip 条目应被拒绝")
	}

	if err := os.Symlink(outsideDir, filepath.Join(destDir, "link-out")); err != nil {
		t.Fatalf("创建 symlink 失败: %v", err)
	}
	tarSymlinkPath := filepath.Join(allowedDir, "symlink.tar.gz")
	writeTestTar(t, tarSymlinkPath, true, map[string]string{"link-out/new.txt": "evil"})
	if err := extractTar(policy, tarSymlinkPath, destDir, true); err == nil {
		t.Fatal("写入解压目录内 symlink 指向的外部新文件应被拒绝")
	}
	if _, err := os.Stat(filepath.Join(outsideDir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("外部目录不应被写入，stat err: %v", err)
	}
}

func TestExtractTar_NormalFile(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "allowed")
	destTarDir := filepath.Join(allowedDir, "extract-tar")
	destTarGzDir := filepath.Join(allowedDir, "extract-targz")
	if err := os.MkdirAll(destTarDir, 0755); err != nil {
		t.Fatalf("创建 tar 解压目录失败: %v", err)
	}
	if err := os.MkdirAll(destTarGzDir, 0755); err != nil {
		t.Fatalf("创建 tar.gz 解压目录失败: %v", err)
	}
	policy := NewPathPolicy([]string{allowedDir})

	tarPath := filepath.Join(allowedDir, "ok.tar")
	writeTestTar(t, tarPath, false, map[string]string{"dir/file.txt": "tar-ok"})
	if err := extractTar(policy, tarPath, destTarDir, false); err != nil {
		t.Fatalf("正常 tar 解压应成功: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(destTarDir, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("读取 tar 解压文件失败: %v", err)
	}
	if string(data) != "tar-ok" {
		t.Fatalf("tar 解压内容不匹配: %q", string(data))
	}

	tarGzPath := filepath.Join(allowedDir, "ok.tar.gz")
	writeTestTar(t, tarGzPath, true, map[string]string{"dir/file.txt": "targz-ok"})
	if err := extractTar(policy, tarGzPath, destTarGzDir, true); err != nil {
		t.Fatalf("正常 tar.gz 解压应成功: %v", err)
	}
	data, err = os.ReadFile(filepath.Join(destTarGzDir, "dir", "file.txt"))
	if err != nil {
		t.Fatalf("读取 tar.gz 解压文件失败: %v", err)
	}
	if string(data) != "targz-ok" {
		t.Fatalf("tar.gz 解压内容不匹配: %q", string(data))
	}
}

// ============================================================
// Health handler 测试
// ============================================================

func TestHandleHealth(t *testing.T) {
	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
	}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	req := httptest.NewRequest("GET", "/internal/v1/health", nil)
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("健康检查应返回 200，实际 %d", rec.Code)
	}

	var resp AgentResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !resp.OK {
		t.Error("健康检查 OK 应为 true")
	}
}

// ============================================================
// Transactions/apply handler 测试
// ============================================================

func TestHandleTransactionApply(t *testing.T) {
	tmpDir := t.TempDir()
	allowedDir := filepath.Join(tmpDir, "nginx")
	os.MkdirAll(allowedDir, 0755)

	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
		Nginx: app.NginxConfig{PanelDir: allowedDir},
	}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	// 覆盖默认的路径策略，使用临时目录
	server.policy = NewPathPolicy([]string{allowedDir})

	targetPath := filepath.Join(allowedDir, "test.conf")
	content := base64.StdEncoding.EncodeToString([]byte("server { listen 80; }"))

	body := fmt.Sprintf(`{
		"operation_id": "op_test_001",
		"changes": [
			{"type": "write", "path": "%s", "content_base64": "%s", "perm": 420}
		]
	}`, targetPath, content)

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("事务应返回 200，实际 %d, body: %s", rec.Code, rec.Body.String())
	}

	var resp AgentResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !resp.OK {
		t.Errorf("事务 OK 应为 true，error: %s", resp.Error)
	}

	// 验证文件已写入
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("读取写入的文件失败: %v", err)
	}
	if string(data) != "server { listen 80; }" {
		t.Errorf("文件内容不匹配: %q", string(data))
	}
}

func TestHandleTransactionApply_InvalidPath(t *testing.T) {
	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
		Nginx: app.NginxConfig{PanelDir: "/opt/nxpanel/nginx"},
	}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	body := `{
		"operation_id": "op_test_002",
		"changes": [
			{"type": "write", "path": "/etc/passwd", "content_base64": "ZXZpbA==", "perm": 420}
		]
	}`

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("非法路径应返回 500，实际 %d", rec.Code)
	}
}

func TestHandleTransactionApply_InvalidToken(t *testing.T) {
	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "correct-token"},
	}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "wrong-token")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("错误 token 应返回 401，实际 %d", rec.Code)
	}
}

func TestHandleTransactionApply_EmptyOperationID(t *testing.T) {
	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
	}
	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	body := `{"operation_id": "", "changes": [{"type": "write", "path": "/tmp/test"}]}`

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("空 operation_id 应返回 400，实际 %d", rec.Code)
	}
}

func writeTestZip(t *testing.T, archivePath string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("创建测试 zip 失败: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("创建 zip 条目失败: %v", err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("写入 zip 条目失败: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 zip writer 失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭 zip 文件失败: %v", err)
	}
}

func writeTestTar(t *testing.T, archivePath string, gzipped bool, entries map[string]string) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatalf("创建测试 tar 失败: %v", err)
	}
	var writer io.Writer = f
	var gzWriter *gzip.Writer
	if gzipped {
		gzWriter = gzip.NewWriter(f)
		writer = gzWriter
	}
	tw := tar.NewWriter(writer)
	for name, content := range entries {
		header := &tar.Header{Name: name, Mode: 0644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("写入 tar 头失败: %v", err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatalf("写入 tar 条目失败: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("关闭 tar writer 失败: %v", err)
	}
	if gzWriter != nil {
		if err := gzWriter.Close(); err != nil {
			t.Fatalf("关闭 gzip writer 失败: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("关闭 tar 文件失败: %v", err)
	}
}

// stringReader 辅助函数
func stringReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
