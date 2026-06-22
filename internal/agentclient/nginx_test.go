// agentclient 包集成测试 — B5 Nginx 操作方法
//
// 通过临时 Unix Socket 测试完整的 nginx detect/test/reload/ensure-include 通信。
package agentclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/agent"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginx"
)

// createFakeNginxForIntegration 创建测试用的 fake nginx 和完整环境
func createFakeNginxForIntegration(t *testing.T) (binPath, confPath, panelDir string) {
	t.Helper()
	tmpDir := t.TempDir()

	// 创建 fake nginx 脚本（成功模式）
	binPath = filepath.Join(tmpDir, "sbin", "nginx")
	os.MkdirAll(filepath.Dir(binPath), 0755)
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-V" ]; then
    echo "nginx version: nginx/1.24.0-fake" >&2
    echo "configure arguments: --prefix=/etc/nginx --conf-path=%s/nginx.conf" >&2
    exit 0
elif [ "$1" = "-t" ]; then
    echo "nginx: the configuration file syntax is ok" >&2
    echo "nginx: configuration file test is successful" >&2
    exit 0
elif [ "$1" = "-s" ]; then
    exit 0
fi
exit 0
`, tmpDir)
	os.WriteFile(binPath, []byte(script), 0755)

	// 创建 fake 配置文件
	confPath = filepath.Join(tmpDir, "nginx.conf")
	content := "http {\n  include /etc/nginx/conf.d/*.conf;\n}\n"
	os.WriteFile(confPath, []byte(content), 0644)

	// 创建 conf.d 目录
	os.MkdirAll(filepath.Join(tmpDir, "conf.d"), 0755)

	// 面板目录
	panelDir = filepath.Join(tmpDir, "panel")
	os.MkdirAll(panelDir, 0755)

	return binPath, confPath, panelDir
}

// setupB5TestAgent 启动带 nginx 能力的测试 agent
func setupB5TestAgent(t *testing.T) (socketPath, token string, binPath string) {
	t.Helper()

	bin, confPath, panelDir := createFakeNginxForIntegration(t)
	token = "b5-test-token"
	socketPath = filepath.Join(t.TempDir(), "b5-agent.sock")

	cfg := &app.Config{
		Agent: app.AgentConfig{
			Token:      token,
			SocketPath: socketPath,
		},
		Nginx: app.NginxConfig{
			Bin:      bin,
			ConfPath: confPath,
			PanelDir: panelDir,
		},
	}

	agentServer, err := agent.NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 agent server 失败: %v", err)
	}

	nginx.InitTemplates("../../configs/templates")

	// 覆盖路径策略
	agentServer.SetPolicyForTest(agent.NewPathPolicy([]string{
		panelDir,
		filepath.Dir(confPath),
		"/etc/nginx/conf.d",
	}))

	// 启动 Unix Socket 服务器
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("监听失败: %v", err)
	}

	httpServer := &http.Server{Handler: agentServer.Handler()}
	go func() {
		_ = httpServer.Serve(listener)
	}()

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		_ = os.Remove(socketPath)
	})

	time.Sleep(10 * time.Millisecond)
	return socketPath, token, bin
}

// TestClient_DetectNginx 测试 Nginx 检测
func TestClient_DetectNginx(t *testing.T) {
	socketPath, token, binPath := setupB5TestAgent(t)
	client := NewWithDefaults(socketPath, token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.DetectNginx(ctx, &NginxDetectRequest{
		NginxBin: binPath,
	})
	if err != nil {
		t.Fatalf("DetectNginx 失败: %v", err)
	}

	if !resp.TestOK {
		t.Error("TestOK 应为 true")
	}
	if resp.Version == "" {
		t.Error("Version 不应为空")
	}
}

// TestClient_TestNginx 测试 nginx -t
func TestClient_TestNginx(t *testing.T) {
	socketPath, token, _ := setupB5TestAgent(t)
	client := NewWithDefaults(socketPath, token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.TestNginx(ctx)
	if err != nil {
		t.Fatalf("TestNginx 失败: %v", err)
	}

	if !resp.OK {
		t.Error("OK 应为 true")
	}
}

// TestClient_ReloadNginx 测试 nginx reload
func TestClient_ReloadNginx(t *testing.T) {
	socketPath, token, _ := setupB5TestAgent(t)
	client := NewWithDefaults(socketPath, token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ReloadNginx(ctx, &NginxReloadRequest{
		TestBeforeReload: true,
	})
	if err != nil {
		t.Fatalf("ReloadNginx 失败: %v", err)
	}

	if !resp.OK {
		t.Error("OK 应为 true")
	}
}

// TestClient_EnsureInclude 测试安装 include 入口
func TestClient_EnsureInclude(t *testing.T) {
	socketPath, token, _ := setupB5TestAgent(t)
	client := NewWithDefaults(socketPath, token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.EnsureInclude(ctx, &EnsureIncludeRequest{
		ConfirmModifyMainConf: true,
	})
	if err != nil {
		t.Fatalf("EnsureInclude 失败: %v", err)
	}

	if !resp.Installed {
		t.Error("Installed 应为 true")
	}
}

// TestClient_DetectNginx_InvalidToken 测试错误 token
func TestClient_DetectNginx_InvalidToken(t *testing.T) {
	socketPath, _, _ := setupB5TestAgent(t)
	client := NewWithDefaults(socketPath, "wrong-token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.DetectNginx(ctx, &NginxDetectRequest{})
	if err == nil {
		t.Error("错误 token 应返回错误")
	}
}
