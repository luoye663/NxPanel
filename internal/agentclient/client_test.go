// agentclient 包集成测试 — 使用临时 Unix Socket 测试完整通信
package agentclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/accessanalysis"
	"github.com/luoye663/nxpanel/internal/agent"
	"github.com/luoye663/nxpanel/internal/app"
)

type closeTrackingTransport struct {
	closes atomic.Int32
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func (*closeTrackingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("unused")
}

func (t *closeTrackingTransport) CloseIdleConnections() {
	t.closes.Add(1)
}

func TestClientCloseIsIdempotent(t *testing.T) {
	transport := &closeTrackingTransport{}
	client := &Client{httpClient: &http.Client{Transport: transport}}
	client.CloseIdleConnections()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if got := transport.closes.Load(); got != 1 {
		t.Fatalf("transport close count = %d, want 1", got)
	}
}

func TestAccessAnalysisScanDirectBoundedEnvelopeDecode(t *testing.T) {
	want := accessanalysis.AgentScanResponse{ScannedLines: 7, Paths: []accessanalysis.PathStat{{Date: "2026-07-26", Path: "/ok", Requests: 3, LastSeenAt: "2026-07-26T01:02:03Z"}}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte("{\"ok\":true,\"data\":"), data...)
	body = append(body, '}')
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/internal/v1/logs/access-analysis/scan" {
			t.Fatalf("path=%s", req.URL.Path)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}}
	got, err := client.AccessAnalysisScan(context.Background(), &accessanalysis.AgentScanRequest{MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if got.ScannedLines != want.ScannedLines || len(got.Paths) != 1 || got.Paths[0].Path != "/ok" {
		t.Fatalf("decoded response=%+v", got)
	}
}

func TestAccessAnalysisScanRejectsOverLimitResponse(t *testing.T) {
	req := &accessanalysis.AgentScanRequest{MaxBytes: 1}
	limit := accessAnalysisResponseLimit(req)
	body := "{\"ok\":true,\"data\":{\"parse_errors\":[\"" + strings.Repeat("x", int(limit)) + "\"]}}"
	client := &Client{httpClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	if _, err := client.AccessAnalysisScan(context.Background(), req); err == nil || !strings.Contains(err.Error(), "超过") {
		t.Fatalf("over-limit error=%v", err)
	}
}

// setupTestAgent 启动测试用的 agent 服务器
// 返回 socket 路径、token 和 cleanup 函数
func setupTestAgent(t *testing.T, allowedDir string) (socketPath, token string) {
	t.Helper()

	token = "test-agent-token-for-integration"
	socketPath = filepath.Join(t.TempDir(), "agent.sock")

	cfg := &app.Config{
		Agent: app.AgentConfig{
			Token:      token,
			SocketPath: socketPath,
		},
		Nginx: app.NginxConfig{
			PanelDir: allowedDir,
		},
	}

	agentServer, err := agent.NewServer(cfg)
	if err != nil {
		t.Fatalf("创建 agent server 失败: %v", err)
	}

	// 覆盖路径策略，使用测试的允许目录
	agentServer.SetPolicyForTest(agent.NewPathPolicy([]string{allowedDir}))

	// 在 Unix Socket 上监听
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("监听 Unix Socket 失败: %v", err)
	}

	httpServer := &http.Server{Handler: agentServer.Handler()}

	// 在后台启动服务器
	go func() {
		_ = httpServer.Serve(listener)
	}()

	// 确保测试结束时关闭
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		_ = os.Remove(socketPath)
	})

	// 等待服务器启动
	time.Sleep(10 * time.Millisecond)

	return socketPath, token
}

// TestClient_Health 测试通过 Unix Socket 调用健康检查
func TestClient_Health(t *testing.T) {
	allowedDir := t.TempDir()
	socketPath, token := setupTestAgent(t, allowedDir)

	client := NewWithDefaults(socketPath, token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("健康检查失败: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status 期望 ok，实际 %s", resp.Status)
	}
	if resp.Service != "nxpanel-agent" {
		t.Errorf("service 期望 nxpanel-agent，实际 %s", resp.Service)
	}
}

// TestClient_HealthInvalidToken 测试错误 token 被拒绝
func TestClient_HealthInvalidToken(t *testing.T) {
	allowedDir := t.TempDir()
	socketPath, _ := setupTestAgent(t, allowedDir)

	// 使用错误的 token
	client := NewWithDefaults(socketPath, "wrong-token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Health(ctx)
	if err == nil {
		t.Error("错误 token 应返回错误")
	}
}

func TestClient_FilesUploadStream(t *testing.T) {
	allowedDir := t.TempDir()
	socketPath, token := setupTestAgent(t, allowedDir)
	client := NewWithDefaults(socketPath, token)
	target := filepath.Join(allowedDir, "streamed.txt")
	if err := client.FilesUploadStream(context.Background(), target, strings.NewReader("streamed content"), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "streamed content" {
		t.Fatalf("content = %q", content)
	}
}

// TestClient_ApplyTransaction 测试文件事务
func TestClient_ApplyTransaction(t *testing.T) {
	allowedDir := t.TempDir()
	socketPath, token := setupTestAgent(t, allowedDir)

	client := NewWithDefaults(socketPath, token)

	targetPath := filepath.Join(allowedDir, "test-site.conf")
	content := base64.StdEncoding.EncodeToString([]byte("server { listen 80; }"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: "op_test_integration",
		Changes: []FileChangeRequest{
			{
				Type:          "write",
				Path:          targetPath,
				ContentBase64: content,
				Perm:          0644,
			},
		},
	})
	if err != nil {
		t.Fatalf("事务失败: %v", err)
	}

	// 验证返回了备份记录
	if len(result.Backups) != 1 {
		t.Errorf("期望 1 个备份，实际 %d", len(result.Backups))
	}

	// 验证文件已写入
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(data) != "server { listen 80; }" {
		t.Errorf("文件内容不匹配: %q", string(data))
	}
}

// TestClient_ApplyTransactionMultipleChanges 测试多文件事务
func TestClient_ApplyTransactionMultipleChanges(t *testing.T) {
	allowedDir := t.TempDir()
	sitesAvail := filepath.Join(allowedDir, "sites-available")
	sitesEnabled := filepath.Join(allowedDir, "sites-enabled")
	os.MkdirAll(sitesAvail, 0755)
	os.MkdirAll(sitesEnabled, 0755)

	socketPath, token := setupTestAgent(t, allowedDir)
	client := NewWithDefaults(socketPath, token)

	confPath := filepath.Join(sitesAvail, "example.com.conf")
	linkPath := filepath.Join(sitesEnabled, "example.com.conf")
	content := base64.StdEncoding.EncodeToString([]byte("server { listen 80; server_name example.com; }"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: "op_multi_test",
		Changes: []FileChangeRequest{
			{
				Type:          "write",
				Path:          confPath,
				ContentBase64: content,
				Perm:          0644,
			},
			{
				Type:   "symlink",
				Path:   linkPath,
				Target: "../sites-available/example.com.conf",
			},
		},
	})
	if err != nil {
		t.Fatalf("事务失败: %v", err)
	}

	if len(result.Backups) != 2 {
		t.Errorf("期望 2 个备份，实际 %d", len(result.Backups))
	}

	// 验证符号链接
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("读取符号链接失败: %v", err)
	}
	if target != "../sites-available/example.com.conf" {
		t.Errorf("符号链接目标不匹配: %s", target)
	}
}

// TestClient_ApplyTransactionInvalidPath 测试非法路径被拒绝
func TestClient_ApplyTransactionInvalidPath(t *testing.T) {
	allowedDir := t.TempDir()
	socketPath, token := setupTestAgent(t, allowedDir)

	client := NewWithDefaults(socketPath, token)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: "op_evil",
		Changes: []FileChangeRequest{
			{
				Type:          "write",
				Path:          "/etc/passwd",
				ContentBase64: base64.StdEncoding.EncodeToString([]byte("evil")),
				Perm:          0644,
			},
		},
	})
	if err == nil {
		t.Error("非法路径应返回错误")
	}
}

// TestClient_ConnectionRefused 测试连接失败的错误处理
func TestClient_ConnectionRefused(t *testing.T) {
	client := NewWithDefaults("/tmp/nonexistent-agent.sock", "any-token")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.Health(ctx)
	if err == nil {
		t.Error("不存在的 socket 应返回错误")
	}
}

// TestClient_ApplyTransactionWithCreateDir 测试 write 操作自动创建父目录
func TestClient_ApplyTransactionWithCreateDir(t *testing.T) {
	allowedDir := t.TempDir()
	socketPath, token := setupTestAgent(t, allowedDir)

	client := NewWithDefaults(socketPath, token)

	// 写入一个深层路径的文件（父目录不存在）
	deepPath := filepath.Join(allowedDir, "ssl", "site_001", "fullchain.pem")
	content := base64.StdEncoding.EncodeToString([]byte("cert-content"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: fmt.Sprintf("op_deep_%d", time.Now().UnixNano()),
		Changes: []FileChangeRequest{
			{
				Type:          "write",
				Path:          deepPath,
				ContentBase64: content,
				Perm:          0644,
			},
		},
	})
	if err != nil {
		t.Fatalf("深层路径写入失败: %v", err)
	}

	// 验证文件已写入
	data, err := os.ReadFile(deepPath)
	if err != nil {
		t.Fatalf("读取文件失败: %v", err)
	}
	if string(data) != "cert-content" {
		t.Errorf("文件内容不匹配: %q", string(data))
	}
}
