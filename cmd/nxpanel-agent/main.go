// nxpanel-agent — root 权限代理服务入口
// 以 root 运行，监听 Unix Socket，执行 Nginx 特权操作
// 不暴露公网端口，只接受本机 API 调用
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/luoye663/nxpanel/internal/agent"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginx"
)

func main() {
	// 解析命令行参数
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	migrateConfig := flag.Bool("migrate-config", false, "迁移配置文件（添加缺失字段、标记废弃字段）")
	showVersion := flag.Bool("version", false, "显示版本信息")
	flag.Parse()

	// 显示版本信息
	if *showVersion {
		fmt.Println(app.Version)
		os.Exit(0)
	}

	// 配置迁移模式
	if *migrateConfig {
		// 自动查找配置文件
		finalConfigPath, err := app.FindConfigFile(*configPath)
		if err != nil {
			slog.Error("查找配置文件失败", "error", err)
			os.Exit(1)
		}

		if err := agent.MigrateConfig(finalConfigPath); err != nil {
			slog.Error("配置迁移失败", "error", err)
			os.Exit(1)
		}

		fmt.Println("配置迁移完成")
		os.Exit(0)
	}

	// 自动查找配置文件
	finalConfigPath, err := app.FindConfigFile(*configPath)
	if err != nil {
		slog.Error("查找配置文件失败", "error", err)
		slog.Info("提示：将配置文件放在可执行文件父目录的 config/ 子目录，或使用 -config 参数指定绝对路径")
		os.Exit(1)
	}
	slog.Info("使用配置文件", "path", finalConfigPath)

	// 加载配置
	cfg, err := app.LoadConfig(finalConfigPath)
	if err != nil {
		slog.Error("加载配置失败", "error", err)
		os.Exit(1)
	}

	// 确保数据目录存在
	if err := os.MkdirAll(cfg.DataDir, 0755); err != nil {
		slog.Error("创建数据目录失败", "dir", cfg.DataDir, "error", err)
		os.Exit(1)
	}

	// 初始化结构化日志（同时输出到 stdout 和文件）
	agentLogFile := cfg.AgentLogPath()
	logger, logCloser := app.InitLoggerToFileWithRotation(cfg.LogLevel, cfg.LogFormat, agentLogFile, cfg.ServiceLogRotate)
	defer logCloser.Close()
	slog.SetDefault(logger)

	slog.Info("nxpanel-agent 正在启动",
		"socket", cfg.Agent.SocketPath,
		"log_file", agentLogFile,
		"service_log_rotate_enabled", cfg.ServiceLogRotate.Enabled,
		"service_log_rotate_max_size", cfg.ServiceLogRotate.MaxSize,
	)

	// 确保 socket 目录存在
	socketDir := cfg.Agent.SocketPath
	if lastSlash := len(socketDir) - 1; lastSlash >= 0 {
		for i := lastSlash; i >= 0; i-- {
			if socketDir[i] == '/' {
				socketDir = socketDir[:i]
				break
			}
		}
	}
	if err := os.MkdirAll(socketDir, 0770); err != nil {
		slog.Error("创建 socket 目录失败", "dir", socketDir, "error", err)
		os.Exit(1)
	}

	// 如果已存在旧的 socket 文件，先删除
	if _, err := os.Stat(cfg.Agent.SocketPath); err == nil {
		_ = os.Remove(cfg.Agent.SocketPath)
	}

	// 自动查找 Nginx 模板目录
	cfg.Nginx.TemplatesDir = app.FindTemplatesDir(cfg.Nginx.TemplatesDir)

	// 加载 Nginx 模板
	if err := nginx.InitTemplates(cfg.Nginx.TemplatesDir); err != nil {
		slog.Error("加载 Nginx 模板失败", "error", err)
		os.Exit(1)
	}

	// 初始化 Nginx 路径校验器和 web 用户候选列表
	nginx.SetAllowedRootPrefixes(cfg.Nginx.AllowedRootPrefixes)
	nginx.SetAllowedLogPrefixes(cfg.Nginx.AllowedLogPrefixes)
	nginx.SetWebUserOptions(cfg.Nginx.WebUserCandidates)

	// 创建 agent 服务器（后续 Phase 会补充完整初始化逻辑）
	if cfg.Agent.Token == "" {
		slog.Error("agent.token 不能为空，请在配置文件中设置或通过环境变量 NXPANEL_AGENT_TOKEN 指定")
		os.Exit(1)
	}

	agentServer, err := agent.NewServer(cfg)
	if err != nil {
		slog.Error("创建 Agent 服务器失败", "error", err)
		os.Exit(1)
	}

	agentServer.AutoDetectIfNeeded()

	// 在 Unix Socket 上监听
	listener, err := net.Listen("unix", cfg.Agent.SocketPath)
	if err != nil {
		slog.Error("监听 Unix Socket 失败", "error", err)
		os.Exit(1)
	}
	// 设置 socket 文件权限：仅 owner 和 group 可读写
	if err := os.Chmod(cfg.Agent.SocketPath, 0660); err != nil {
		slog.Error("设置 socket 权限失败", "error", err)
		os.Exit(1)
	}

	// 设置 socket 文件的 group，允许 API 进程（非 root）连接
	if cfg.Agent.SocketGroup != "" {
		gr, err := user.LookupGroup(cfg.Agent.SocketGroup)
		if err != nil {
			slog.Error("查找 socket_group 失败", "group", cfg.Agent.SocketGroup, "error", err)
			os.Exit(1)
		}
		gid, _ := strconv.Atoi(gr.Gid)
		if err := os.Chown(cfg.Agent.SocketPath, -1, gid); err != nil {
			slog.Error("设置 socket group 失败", "group", cfg.Agent.SocketGroup, "error", err)
			os.Exit(1)
		}
		slog.Info("socket 文件权限已设置", "path", cfg.Agent.SocketPath, "mode", "0660", "group", cfg.Agent.SocketGroup)
	} else {
		slog.Warn("未配置 agent.socket_group，非 root 进程可能无法连接 socket")
	}

	httpServer := &http.Server{
		Handler: agentServer.Handler(),
	}

	// 优雅关闭
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("收到关闭信号，开始优雅关闭", "signal", sig.String())

		ctx, cancel := context.WithTimeout(context.Background(), app.ParseDurationOrDefault(cfg.Agent.ShutdownTimeout, 10*time.Second))
		defer cancel()

		_ = httpServer.Shutdown(ctx)
	}()

	slog.Info("Agent 服务已启动", "socket", cfg.Agent.SocketPath)
	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		slog.Error("Agent 服务异常退出", "error", err)
		os.Exit(1)
	}

	// 清理 socket 文件
	_ = os.Remove(cfg.Agent.SocketPath)
	slog.Info("nxpanel-agent 已停止")
	fmt.Println("nxpanel-agent 已停止")
}
