// api 包的测试
package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
)

// newTestDB 创建测试用的内存 SQLite 数据库并执行迁移
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatalf("测试迁移失败: %v", err)
	}
	return database
}

// newTestServer 创建用于测试的 API 服务器
func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &app.Config{
		LogLevel: "info",
		API:      app.APIConfig{Listen: "127.0.0.1:0", LoginPath: "/nx-testgate"},
		Agent:    app.AgentConfig{SocketPath: "/tmp/test.sock"},
		Nginx:    app.NginxConfig{ConfPath: "/etc/nginx/nginx.conf"},
	}
	database := newTestDB(t)
	server, err := NewServer(cfg, database)
	if err != nil {
		t.Fatalf("创建测试服务器失败: %v", err)
	}
	return server
}

func newTestServerWithAgent(t *testing.T) *Server {
	t.Helper()
	socketDir := t.TempDir()
	socketPath := filepath.Join(socketDir, "agent.sock")
	startTestAgentServer(t, socketPath)
	cfg := &app.Config{
		LogLevel: "info",
		API:      app.APIConfig{Listen: "127.0.0.1:0", LoginPath: "/nx-testgate"},
		Agent:    app.AgentConfig{SocketPath: socketPath},
		Nginx:    app.NginxConfig{ConfPath: "/etc/nginx/nginx.conf"},
	}
	database := newTestDB(t)
	server, err := NewServer(cfg, database)
	if err != nil {
		t.Fatalf("创建测试服务器失败: %v", err)
	}
	return server
}

func startTestAgentServer(t *testing.T, socketPath string) {
	t.Helper()
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("启动测试 Agent 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","service":"nxpanel-agent","version":"test"}}`)
	})}
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	})
	go func() {
		_ = server.Serve(listener)
	}()
	for i := 0; i < 50; i++ {
		conn, dialErr := net.DialTimeout("unix", socketPath, 20*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("测试 Agent 未能在预期时间内启动: %s", socketPath)
}

// TestHealthEndpoint 验证 health 端点返回正确的 JSON
func TestHealthEndpoint(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("默认 health 端点状态码期望 404，实际 %d", rec.Code)
	}
	server.cfg.API.PublicHealth = true
	server.ReloadSecurityConfig(server.cfg)
	req = httptest.NewRequest("GET", "/health", nil)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("公开 health 端点状态码期望 200，实际 %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type 期望 JSON，实际 %s", ct)
	}

	rid := rec.Header().Get("X-Request-ID")
	if rid == "" {
		t.Error("响应头应包含 X-Request-ID")
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("解析响应 JSON 失败: %v", err)
	}

	if !resp.Success {
		t.Error("health 响应 success 应为 true")
	}
	if resp.RequestID == "" {
		t.Error("health 响应应包含 request_id")
	}
	if resp.Error != nil {
		t.Error("health 响应 error 应为 nil")
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatal("health 响应 data 应为对象")
	}
	if data["status"] != "ok" {
		t.Errorf("data.status 期望 ok，实际 %v", data["status"])
	}
	if data["service"] != "nxpanel-api" {
		t.Errorf("data.service 期望 nxpanel-api，实际 %v", data["service"])
	}
}

func TestHiddenGateFixedAPIPathsReturn404(t *testing.T) {
	server := newTestServer(t)
	for _, path := range []string{"/api/v1/auth/login", "/api/v1/setup/admin", "/login", "/setup"} {
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("固定路径 %s 应返回 404，实际 %d", path, rec.Code)
		}
	}
}
