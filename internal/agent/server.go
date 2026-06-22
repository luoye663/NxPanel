// agent 包 — root 权限代理服务
//
// Server 是 agent 的 HTTP 服务器，负责：
//   - 注册 token 认证中间件
//   - 注册所有内部 RPC 路由
//   - 提供 HTTP Handler 供 cmd/nxpanel-agent/main.go 使用
package agent

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/luoye663/nxpanel/internal/app"
)

type Server struct {
	cfg      *app.Config
	router   *chi.Mux
	policy   *PathPolicy
	executor *NginxExecutor
	timeouts NginxTimeouts
}

func NewServer(cfg *app.Config) (*Server, error) {
	timeouts := NginxTimeouts{
		Test:   app.ParseDurationOrDefault(cfg.Nginx.TestTimeout, 15*time.Second),
		Reload: app.ParseDurationOrDefault(cfg.Nginx.ReloadTimeout, 15*time.Second),
		Dump:   app.ParseDurationOrDefault(cfg.Nginx.DumpTimeout, 30*time.Second),
		Reopen: app.ParseDurationOrDefault(cfg.Nginx.TestTimeout, 15*time.Second),
		Detect: app.ParseDurationOrDefault(cfg.Nginx.DetectTimeout, 10*time.Second),
	}
	s := &Server{
		cfg:      cfg,
		router:   chi.NewRouter(),
		policy:   NewPathPolicyFromConfig(cfg),
		executor: NewNginxExecutor(cfg.Nginx.Bin, cfg.Nginx.ConfPath, timeouts),
		timeouts: timeouts,
	}

	s.setupMiddleware()
	s.setupRoutes()

	go s.cleanupBackups()

	taskLogDir := cfg.TaskLogDir()
	if err := os.MkdirAll(taskLogDir, 0755); err != nil {
		slog.Warn("创建任务日志目录失败", "path", taskLogDir, "error", err)
	}

	return s, nil
}

func (s *Server) cleanupBackups() {
	cleanupOldBackupsWithLog(
		s.cfg.Nginx.PanelDir+"/backups",
		s.cfg.Nginx.BackupMaxCount,
		app.ParseDurationOrDefault(s.cfg.Nginx.BackupMaxAge, 168*time.Hour),
		s.cfg.TaskLogDir(),
	)
}

func (s *Server) ReloadConfig() error {
	if s.cfg.ConfigFile == "" {
		return nil
	}
	return s.cfg.ReloadFromDisk()
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) SetPolicyForTest(policy *PathPolicy) {
	s.policy = policy
}

func (s *Server) AutoDetectIfNeeded() {
	if s.cfg.Nginx.Bin == "" || s.cfg.Nginx.ConfPath != "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), app.ParseDurationOrDefault(s.cfg.Nginx.DetectTimeout, 10*time.Second))
	defer cancel()
	result, err := s.executor.Detect(ctx, s.cfg.Nginx.Bin)
	if err != nil {
		slog.Warn("自动检测 Nginx 配置路径失败", "bin", s.cfg.Nginx.Bin, "error", err)
		return
	}
	if result.ConfPath != "" {
		slog.Info("自动检测到 Nginx 配置路径", "conf_path", result.ConfPath)
		s.persistConfig()
	}
}

func (s *Server) PersistConfig() {
	s.persistConfig()
}

func (s *Server) persistConfig() {
	s.cfg.Nginx.Bin = s.executor.GetBin()
	s.cfg.Nginx.ConfPath = s.executor.GetConfPath()
	s.policy = NewPathPolicyFromConfig(s.cfg)
	slog.Info("准备持久化配置", "bin", s.cfg.Nginx.Bin, "conf_path", s.cfg.Nginx.ConfPath, "config_file", s.cfg.ConfigFile)
	if err := s.cfg.WriteBack(); err != nil {
		slog.Error("持久化配置失败", "error", err)
	} else {
		slog.Info("配置已写回文件", "path", s.cfg.ConfigFile)
	}
}

// setupMiddleware 注册 agent 专属中间件
//
// 中间件执行顺序（从外到内）：
//  1. Recoverer — panic 恢复
//  2. Logger — 请求日志
//  3. TokenAuth — agent token 认证
func (s *Server) setupMiddleware() {
	s.router.Use(chiMiddleware.Recoverer)
	s.router.Use(chiMiddleware.Logger)
	s.router.Use(TokenAuth(s.cfg.Agent.Token))
}
