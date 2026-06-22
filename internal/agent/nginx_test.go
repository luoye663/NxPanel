// agent 包测试 — Nginx executor 和 Nginx handlers
//
// 使用 fake nginx shell 脚本模拟成功/失败场景，不依赖真实 Nginx。
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginx"
)

// ============================================================
// 辅助工具：创建 fake nginx 脚本
// ============================================================

// createFakeNginx 创建一个模拟 nginx 命令的 shell 脚本
//
// 参数：
//   - t: testing.T
//   - behavior: "success" 或 "fail"
//
// 返回脚本路径
func createFakeNginx(t *testing.T, behavior string) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "nginx")

	var script string
	switch behavior {
	case "success":
		script = `#!/bin/sh
if [ "$1" = "-V" ]; then
    echo "nginx version: nginx/1.24.0-fake" >&2
    echo "configure arguments: --prefix=/etc/nginx --conf-path=/etc/nginx/nginx.conf" >&2
    exit 0
elif [ "$1" = "-t" ]; then
    if [ "$3" != "" ]; then
        echo "nginx: the configuration file $3 syntax is ok" >&2
        echo "nginx: configuration file $3 test is successful" >&2
    else
        echo "nginx: the configuration file /etc/nginx/nginx.conf syntax is ok" >&2
        echo "nginx: configuration file /etc/nginx/nginx.conf test is successful" >&2
    fi
    exit 0
elif [ "$1" = "-c" ] && [ "$3" = "-s" ]; then
    exit 0
elif [ "$1" = "-s" ]; then
    exit 0
fi
exit 0
`
	case "fail":
		script = `#!/bin/sh
if [ "$1" = "-V" ]; then
    echo "nginx version: nginx/1.24.0-fake" >&2
    echo "configure arguments: --prefix=/etc/nginx --conf-path=/etc/nginx/nginx.conf" >&2
    exit 0
elif [ "$1" = "-t" ]; then
    if [ "$3" != "" ]; then
        echo "nginx: [emerg] unexpected end of file" >&2
    else
        echo "nginx: [emerg] unexpected end of file" >&2
    fi
    exit 1
elif [ "$1" = "-c" ] && [ "$3" = "-s" ]; then
    echo "nginx: [error] invalid PID number" >&2
    exit 1
elif [ "$1" = "-s" ]; then
    echo "nginx: [error] invalid PID number" >&2
    exit 1
fi
exit 1
`
	default:
		t.Fatalf("未知的 fake nginx 行为: %s", behavior)
	}

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("创建 fake nginx 脚本失败: %v", err)
	}
	return scriptPath
}

// createFakeNginxDir 创建一个包含 fake nginx 的目录和配置文件
func createFakeNginxDir(t *testing.T, behavior string) (binPath, confPath, panelDir string) {
	t.Helper()
	tmpDir := t.TempDir()

	// 创建 fake nginx
	binPath = filepath.Join(tmpDir, "sbin", "nginx")
	os.MkdirAll(filepath.Dir(binPath), 0755)
	script := ""
	switch behavior {
	case "success":
		script = fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-V" ]; then
    echo "nginx version: nginx/1.24.0-fake" >&2
    echo "configure arguments: --prefix=%s --conf-path=%s/nginx.conf" >&2
    exit 0
elif [ "$1" = "-T" ]; then
    conf="$3"
    if [ "$conf" = "" ]; then
        conf="%s/nginx.conf"
    fi
    echo "nginx: the configuration file $conf syntax is ok" >&2
    echo "nginx: configuration file $conf test is successful" >&2
    echo "# configuration file $conf:"
    cat "$conf" 2>/dev/null
    exit 0
elif [ "$1" = "-t" ]; then
    if [ "$3" != "" ]; then
        echo "nginx: the configuration file $3 syntax is ok" >&2
        echo "nginx: configuration file $3 test is successful" >&2
    else
        echo "nginx: the configuration file %s/nginx.conf syntax is ok" >&2
        echo "nginx: the configuration file %s/nginx.conf test is successful" >&2
    fi
    exit 0
elif [ "$1" = "-s" ]; then
    exit 0
fi
exit 0
`, tmpDir, tmpDir, tmpDir, tmpDir, tmpDir)
	case "fail":
		script = fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-V" ]; then
    echo "nginx version: nginx/1.24.0-fake" >&2
    echo "configure arguments: --prefix=/etc/nginx --conf-path=%s/nginx.conf" >&2
    exit 0
elif [ "$1" = "-t" ]; then
    echo "nginx: [emerg] unexpected end of file, expecting }" >&2
    exit 1
elif [ "$1" = "-s" ]; then
    echo "nginx: [error] invalid PID number" >&2
    exit 1
fi
exit 1
`, tmpDir)
	case "reload_pid_start_success":
		script = fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-V" ]; then
    echo "nginx version: nginx/1.24.0-fake" >&2
    echo "configure arguments: --prefix=%s --conf-path=%s/nginx.conf" >&2
    exit 0
elif [ "$1" = "-t" ]; then
    echo "nginx: the configuration file $3 syntax is ok" >&2
    echo "nginx: configuration file $3 test is successful" >&2
    exit 0
elif [ "$1" = "-c" ] && [ "$3" = "-s" ] && [ "$4" = "reload" ]; then
    echo "nginx: [error] invalid PID number \"\" in \"/var/run/nginx.pid\"" >&2
    exit 1
elif [ "$1" = "-c" ] && [ "$3" = "-s" ]; then
    exit 0
elif [ "$1" = "-c" ]; then
    exit 0
fi
exit 1
`, tmpDir, tmpDir)
	}
	os.WriteFile(binPath, []byte(script), 0755)

	// 创建 fake 配置文件
	confPath = filepath.Join(tmpDir, "nginx.conf")
	os.WriteFile(confPath, []byte("http {\n}\n"), 0644)

	// 创建面板目录
	panelDir = filepath.Join(tmpDir, "panel")
	os.MkdirAll(panelDir, 0755)

	return binPath, confPath, panelDir
}

// ============================================================
// NginxExecutor 测试
// ============================================================

func TestNginxExecutor_Test_Success(t *testing.T) {
	fakeBin := createFakeNginx(t, "success")
	executor := NewNginxExecutorWithDefaults(fakeBin, "/etc/nginx/nginx.conf")

	ctx := context.Background()
	result, err := executor.Test(ctx)
	if err != nil {
		t.Fatalf("期望 test 成功，但失败: %v", err)
	}
	if result.Stderr == "" {
		t.Error("期望有 stderr 输出")
	}
}

func TestNginxExecutor_Test_Fail(t *testing.T) {
	fakeBin := createFakeNginx(t, "fail")
	executor := NewNginxExecutorWithDefaults(fakeBin, "/etc/nginx/nginx.conf")

	ctx := context.Background()
	result, err := executor.Test(ctx)
	if err == nil {
		t.Fatal("期望 test 失败，但成功了")
	}
	if result.Stderr == "" {
		t.Error("失败时应有 stderr 输出")
	}
}

func TestNginxExecutor_Reload_Success(t *testing.T) {
	fakeBin := createFakeNginx(t, "success")
	executor := NewNginxExecutorWithDefaults(fakeBin, "/etc/nginx/nginx.conf")

	ctx := context.Background()
	_, err := executor.Reload(ctx)
	if err != nil {
		t.Fatalf("期望 reload 成功: %v", err)
	}
}

func TestNginxExecutor_ReloadAndReopen_UseConfPath(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "nginx")
	argsPath := filepath.Join(tmpDir, "args.log")
	confPath := filepath.Join(tmpDir, "nginx.conf")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$*" >> %q
exit 0
`, argsPath)
	os.WriteFile(scriptPath, []byte(script), 0755)
	os.WriteFile(confPath, []byte("http {}\n"), 0644)

	executor := NewNginxExecutorWithDefaults(scriptPath, confPath)
	ctx := context.Background()

	if _, err := executor.Reload(ctx); err != nil {
		t.Fatalf("reload 应成功: %v", err)
	}
	if _, err := executor.Reopen(ctx); err != nil {
		t.Fatalf("reopen 应成功: %v", err)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("读取参数日志失败: %v", err)
	}
	got := string(args)
	if !strings.Contains(got, "-c "+confPath+" -s reload") {
		t.Fatalf("reload 未传 -c confPath，args: %s", got)
	}
	if !strings.Contains(got, "-c "+confPath+" -s reopen") {
		t.Fatalf("reopen 未传 -c confPath，args: %s", got)
	}
}

func TestNginxExecutor_Reload_Fail(t *testing.T) {
	fakeBin := createFakeNginx(t, "fail")
	executor := NewNginxExecutorWithDefaults(fakeBin, "/etc/nginx/nginx.conf")

	ctx := context.Background()
	_, err := executor.Reload(ctx)
	if err == nil {
		t.Fatal("期望 reload 失败")
	}
}

func TestNginxExecutor_Detect_Success(t *testing.T) {
	fakeBin := createFakeNginx(t, "success")
	executor := NewNginxExecutorWithDefaults("", "")

	ctx := context.Background()
	result, err := executor.Detect(ctx, fakeBin)
	if err != nil {
		t.Fatalf("Detect 失败: %v", err)
	}

	if result.Bin != fakeBin {
		t.Errorf("Bin = %q, want %q", result.Bin, fakeBin)
	}
	if result.Version != "nginx/1.24.0-fake" {
		t.Errorf("Version = %q, want %q", result.Version, "nginx/1.24.0-fake")
	}
	if !result.TestOK {
		t.Error("TestOK 应为 true")
	}

	// 验证 executor 内部状态已更新
	if executor.GetBin() != fakeBin {
		t.Errorf("executor.Bin 未更新")
	}
}

func TestNginxExecutor_Detect_NoBinary(t *testing.T) {
	executor := NewNginxExecutorWithDefaults("", "")

	ctx := context.Background()
	_, err := executor.Detect(ctx, "/nonexistent/nginx")
	if err == nil {
		t.Fatal("不存在的二进制应返回错误")
	}
}

func TestNginxExecutor_EmptyBin(t *testing.T) {
	executor := NewNginxExecutorWithDefaults("", "/etc/nginx/nginx.conf")
	ctx := context.Background()

	_, err := executor.Test(ctx)
	if err == nil {
		t.Fatal("空 bin 应返回错误")
	}

	_, err = executor.Reload(ctx)
	if err == nil {
		t.Fatal("空 bin 应返回错误")
	}
}

func TestNginxExecutor_Update(t *testing.T) {
	executor := NewNginxExecutorWithDefaults("/old/bin", "/old/conf")
	executor.Update("/new/bin", "/new/conf")

	if executor.GetBin() != "/new/bin" {
		t.Errorf("GetBin() = %q, want /new/bin", executor.GetBin())
	}
	if executor.GetConfPath() != "/new/conf" {
		t.Errorf("GetConfPath() = %q, want /new/conf", executor.GetConfPath())
	}
}

// ============================================================
// Nginx handlers 测试
// ============================================================

func setupAgentWithFakeNginx(t *testing.T, behavior string) *Server {
	t.Helper()
	binPath, confPath, panelDir := createFakeNginxDir(t, behavior)

	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
		Nginx: app.NginxConfig{
			Bin:      binPath,
			ConfPath: confPath,
			PanelDir: panelDir,
		},
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	// 更新路径策略以包含测试目录
	server.policy = NewPathPolicy([]string{panelDir, filepath.Dir(confPath)})

	return server
}

func TestHandleNginxDetect_Success(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "success")

	body := `{"nginx_bin": ""}`
	req := httptest.NewRequest("POST", "/internal/v1/nginx/detect", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}

	var resp AgentResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !resp.OK {
		t.Errorf("OK 应为 true，error: %s", resp.Error)
	}
}

func TestHandleNginxTest_Success(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "success")

	// 先执行 detect 设置 bin
	server.executor.Update(server.cfg.Nginx.Bin, server.cfg.Nginx.ConfPath)

	req := httptest.NewRequest("POST", "/internal/v1/nginx/test", strings.NewReader(""))
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleNginxTest_Fail(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "fail")

	req := httptest.NewRequest("POST", "/internal/v1/nginx/test", strings.NewReader(""))
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("期望 500，实际 %d", rec.Code)
	}
}

func TestHandleNginxReload_Success(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "success")

	body := `{"test_before_reload": true}`
	req := httptest.NewRequest("POST", "/internal/v1/nginx/reload", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleNginxReload_Fail(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "fail")

	body := `{"test_before_reload": true}`
	req := httptest.NewRequest("POST", "/internal/v1/nginx/reload", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	// fail nginx 的 -t 会失败，导致 reload 也失败
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("期望 500，实际 %d", rec.Code)
	}
}

func TestHandleNginxReload_PIDError_StartFallbackSuccess(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "reload_pid_start_success")

	body := `{"test_before_reload": true}`
	req := httptest.NewRequest("POST", "/internal/v1/nginx/reload", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleNginxEnsureInclude_ConfD(t *testing.T) {
	_, confPath, panelDir := createFakeNginxDir(t, "success")

	nginxContent := "http {\n  include /etc/nginx/conf.d/*.conf;\n}\n"
	os.WriteFile(confPath, []byte(nginxContent), 0644)

	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
		Nginx: app.NginxConfig{
			Bin:      filepath.Join(filepath.Dir(confPath), "sbin", "nginx"),
			ConfPath: confPath,
			PanelDir: panelDir,
		},
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}
	server.policy = NewPathPolicy([]string{panelDir, filepath.Dir(confPath), "/etc/nginx/conf.d"})

	confDPath := filepath.Join(filepath.Dir(confPath), "conf.d")
	os.MkdirAll(confDPath, 0755)

	nginx.InitTemplates("../../configs/templates")

	body := `{"confirm_modify_main_conf": true}`
	req := httptest.NewRequest("POST", "/internal/v1/nginx/ensure-include", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}

	var resp AgentResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.OK {
		t.Errorf("OK 应为 true，error: %s", resp.Error)
	}
}

func TestValidateConfigWriteBackField_InvalidValues(t *testing.T) {
	tests := []configWriteBackField{
		{Key: "nginx.log_rotate_interval", Value: "1h"},
		{Key: "api.rate_limit.window", Value: "not-duration"},
		{Key: "api.captcha.provider", Value: "recaptcha"},
		{Key: "api.trusted_proxies", Value: "127.0.0.1,not-an-ip"},
		{Key: "api.tls.cert", Value: "relative/cert.pem"},
		{Key: "api.tls.key", Value: "/etc/key.pem\nmalicious"},
		{Key: "nginx.web_user", Value: "root"},
		{Key: "nginx.web_group", Value: ""},
	}

	for _, tt := range tests {
		t.Run(tt.Key+"="+tt.Value, func(t *testing.T) {
			if err := validateConfigWriteBackField(tt); err == nil {
				t.Fatalf("字段 %s=%q 应校验失败", tt.Key, tt.Value)
			}
		})
	}
}

func TestValidateConfigWriteBackField_ValidValues(t *testing.T) {
	tests := []configWriteBackField{
		{Key: "api.trusted_proxies", Value: "127.0.0.1,10.0.0.0/8,::1"},
		{Key: "api.captcha.provider", Value: "turnstile"},
		{Key: "api.tls.cert", Value: "/etc/nxpanel/tls/cert.pem"},
		{Key: "nginx.web_user", Value: "www-data"},
		{Key: "nginx.web_group", Value: "www-data"},
	}

	for _, tt := range tests {
		t.Run(tt.Key+"="+tt.Value, func(t *testing.T) {
			if err := validateConfigWriteBackField(tt); err != nil {
				t.Fatalf("字段 %s=%q 应校验通过: %v", tt.Key, tt.Value, err)
			}
		})
	}
}

func TestHandleConfigWriteBack_InvalidValueNoPartialUpdate(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "success")
	server.cfg.API.MaxSessions = 5

	body := `{"fields":[{"key":"api.max_sessions","value":"9"},{"key":"api.captcha.provider","value":"recaptcha"}]}`
	req := httptest.NewRequest("POST", "/internal/v1/config/write-back", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
	if server.cfg.API.MaxSessions != 5 {
		t.Fatalf("校验失败时不应部分更新 max_sessions，实际 %d", server.cfg.API.MaxSessions)
	}
}

func TestHandleNginxDetect_OverridesRootWebUser(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "success")
	server.cfg.Nginx.WebUser = "root"
	server.cfg.Nginx.WebGroup = "root"
	if err := os.WriteFile(server.cfg.Nginx.ConfPath, []byte("user www-data;\nhttp {}\n"), 0644); err != nil {
		t.Fatalf("写入 fake nginx.conf 失败: %v", err)
	}

	body := `{"nginx_bin":""}`
	req := httptest.NewRequest("POST", "/internal/v1/nginx/detect", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
	if server.cfg.Nginx.WebUser != "www-data" || server.cfg.Nginx.WebGroup != "www-data" {
		t.Fatalf("detect 应覆盖错误 root web 用户，got %s:%s", server.cfg.Nginx.WebUser, server.cfg.Nginx.WebGroup)
	}
}

// ============================================================
// 事务 handler 的 nginx test/reload 集成测试
// ============================================================

func TestHandleTransactionApply_WithTestNginx_Success(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "success")
	allowedDir := server.cfg.Nginx.PanelDir
	os.MkdirAll(allowedDir, 0755)

	targetPath := filepath.Join(allowedDir, "test.conf")
	body := fmt.Sprintf(`{
		"operation_id": "op_test_nginx_test",
		"changes": [{"type": "write", "path": "%s", "content_base64": "c2VydmVyIHsgfQ==", "perm": 420}],
		"test_nginx": true
	}`, targetPath)

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTransactionApply_WithTestNginx_Fail(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "fail")
	allowedDir := server.cfg.Nginx.PanelDir
	os.MkdirAll(allowedDir, 0755)

	targetPath := filepath.Join(allowedDir, "test.conf")
	body := fmt.Sprintf(`{
		"operation_id": "op_test_nginx_fail",
		"changes": [{"type": "write", "path": "%s", "content_base64": "c2VydmVyIHsgfQ==", "perm": 420}],
		"test_nginx": true
	}`, targetPath)

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("期望 500（nginx -t 失败），实际 %d", rec.Code)
	}

	// 验证文件已被回滚（删除）
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Error("nginx -t 失败后应回滚文件")
	}
}

func TestHandleTransactionApply_WithReloadNginx_Success(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "success")
	allowedDir := server.cfg.Nginx.PanelDir
	os.MkdirAll(allowedDir, 0755)

	targetPath := filepath.Join(allowedDir, "test.conf")
	body := fmt.Sprintf(`{
		"operation_id": "op_test_nginx_reload",
		"changes": [{"type": "write", "path": "%s", "content_base64": "c2VydmVyIHsgfQ==", "perm": 420}],
		"test_nginx": true,
		"reload_nginx": true
	}`, targetPath)

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleTransactionApply_ReloadPIDError_StartFallbackSuccess(t *testing.T) {
	server := setupAgentWithFakeNginx(t, "reload_pid_start_success")
	allowedDir := server.cfg.Nginx.PanelDir
	os.MkdirAll(allowedDir, 0755)

	targetPath := filepath.Join(allowedDir, "test.conf")
	body := fmt.Sprintf(`{
		"operation_id": "op_test_nginx_reload_start_fallback",
		"changes": [{"type": "write", "path": "%s", "content_base64": "c2VydmVyIHsgfQ==", "perm": 420}],
		"test_nginx": true,
		"reload_nginx": true
	}`, targetPath)

	req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", stringReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-NxPanel-Agent-Token", "test-token")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("期望 200，实际 %d，body: %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(targetPath); err != nil {
		t.Errorf("start fallback 成功后文件应保留: %v", err)
	}
}

// stringReader 已在 agent_test.go 中定义，这里不需要重复

func TestNginxExecutor_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "nginx")
	// 使用一个会 sleep 足够长时间的脚本
	script := `#!/bin/sh
sleep 5
exit 0
`
	os.WriteFile(scriptPath, []byte(script), 0755)

	executor := NewNginxExecutorWithDefaults(scriptPath, "/etc/nginx/nginx.conf")

	// 使用极短超时的 context，确保命令来不及完成
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := executor.Test(ctx)
	if err == nil {
		t.Error("超时应返回错误")
	}
}

func TestAutoDetectIfNeeded_WithBinNoConfPath(t *testing.T) {
	binPath, confPath, panelDir := createFakeNginxDir(t, "success")

	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
		Nginx: app.NginxConfig{
			Bin:      binPath,
			ConfPath: "",
			PanelDir: panelDir,
		},
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	if server.executor.GetConfPath() != "" {
		t.Fatal("初始 confPath 应为空")
	}

	server.AutoDetectIfNeeded()

	gotConfPath := server.executor.GetConfPath()
	if gotConfPath != confPath {
		t.Errorf("AutoDetect 后 confPath = %q, want %q", gotConfPath, confPath)
	}
	if _, err := server.policy.Validate(confPath); err != nil {
		t.Fatalf("AutoDetect 后应立即刷新白名单: %v", err)
	}
}

func TestAutoDetectIfNeeded_ConfPathAlreadySet(t *testing.T) {
	fakeBin := createFakeNginx(t, "success")

	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
		Nginx: app.NginxConfig{
			Bin:      fakeBin,
			ConfPath: "/custom/nginx.conf",
			PanelDir: t.TempDir(),
		},
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	server.AutoDetectIfNeeded()

	if server.executor.GetConfPath() != "/custom/nginx.conf" {
		t.Errorf("confPath 已有值时不应该被覆盖，got %q", server.executor.GetConfPath())
	}
}

func TestAutoDetectIfNeeded_NoBin(t *testing.T) {
	cfg := &app.Config{
		Agent: app.AgentConfig{Token: "test-token"},
		Nginx: app.NginxConfig{
			Bin:      "",
			ConfPath: "",
			PanelDir: t.TempDir(),
		},
	}

	server, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 server 失败: %v", err)
	}

	server.AutoDetectIfNeeded()

	if server.executor.GetConfPath() != "" {
		t.Error("无 bin 时不应该执行 detect")
	}
}

func TestNginxExecutor_Test_EmptyConfPath(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "nginx")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-t" ]; then
    if [ "$2" = "-c" ]; then
        echo "ERROR: should not pass -c" >&2
        exit 1
    fi
    echo "nginx: the configuration file %s/nginx.conf syntax is ok" >&2
    echo "nginx: configuration file %s/nginx.conf test is successful" >&2
    exit 0
fi
exit 0
`, tmpDir, tmpDir)
	os.WriteFile(scriptPath, []byte(script), 0755)

	executor := NewNginxExecutorWithDefaults(scriptPath, "")

	ctx := context.Background()
	result, err := executor.Test(ctx)
	if err != nil {
		t.Fatalf("期望 test 成功: %v", err)
	}
	if result.Stderr == "" {
		t.Error("期望有 stderr 输出")
	}
}

func TestNginxExecutor_Detect_ParseTestOutputFallback(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "nginx")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-V" ]; then
    echo "nginx version: nginx/1.24.0-fake" >&2
    echo "configure arguments: --prefix=/usr/local/openresty/nginx" >&2
    exit 0
elif [ "$1" = "-t" ] && [ "$2" != "-c" ]; then
    echo "nginx: the configuration file %s/nginx.conf syntax is ok" >&2
    echo "nginx: configuration file %s/nginx.conf test is successful" >&2
    exit 0
elif [ "$1" = "-t" ] && [ "$2" = "-c" ]; then
    echo "nginx: the configuration file $3 syntax is ok" >&2
    echo "nginx: configuration file $3 test is successful" >&2
    exit 0
fi
exit 0
`, tmpDir, tmpDir)
	os.WriteFile(scriptPath, []byte(script), 0755)

	confPath := filepath.Join(tmpDir, "nginx.conf")
	os.WriteFile(confPath, []byte("http {\n}\n"), 0644)

	executor := NewNginxExecutorWithDefaults("", "")

	ctx := context.Background()
	result, err := executor.Detect(ctx, scriptPath)
	if err != nil {
		t.Fatalf("Detect 失败: %v", err)
	}

	if result.ConfPath != confPath {
		t.Errorf("ConfPath = %q, want %q (should be detected via -t fallback)", result.ConfPath, confPath)
	}
	if !result.TestOK {
		t.Error("TestOK 应为 true")
	}
}
