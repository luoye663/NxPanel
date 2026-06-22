// nxpanel-api — 面板 API 服务入口
// 非 root 运行，提供 Web/API，管理 SQLite，通过 Unix Socket 调用 agent
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
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/luoye663/nxpanel/internal/api"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/nginx"
)

func main() {
	// 解析命令行参数：配置文件路径，默认为 config.yaml
	configPath := flag.String("config", "config.yaml", "配置文件路径")
	showVersion := flag.Bool("version", false, "显示版本信息")
	flag.Parse()

	if *showVersion {
		fmt.Println(app.Version)
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
	apiLogFile := cfg.APILogPath()
	logger, logCloser := app.InitLoggerToFileWithRotation(cfg.LogLevel, cfg.LogFormat, apiLogFile, cfg.ServiceLogRotate)
	defer logCloser.Close()
	slog.SetDefault(logger)

	slog.Info("nxpanel-api 正在启动",
		"listen", cfg.API.Listen,
		"log_file", apiLogFile,
		"service_log_rotate_enabled", cfg.ServiceLogRotate.Enabled,
		"service_log_rotate_max_size", cfg.ServiceLogRotate.MaxSize,
	)
	slog.Info("登录入口提示：如忘记路径，可在启动日志中查找 login_path；Docker 可执行 docker logs nxpanel 2>&1 | grep login_path", "login_path", cfg.API.LoginPath, "访问地址", panelAccessURL(cfg.API.Listen, cfg.API.TLS.Enabled, cfg.API.LoginPath))

	// 打开 SQLite 数据库
	dbPath := filepath.Join(cfg.DataDir, "panel.db")
	dsn := db.DSNFromPath(dbPath, cfg.Database.BusyTimeout)
	database, err := db.Open(dsn)
	if err != nil {
		slog.Error("打开数据库失败", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	// 执行数据库迁移
	if err := db.RunMigrations(database); err != nil {
		slog.Error("数据库迁移失败", "error", err)
		os.Exit(1)
	}

	// 自动查找 Nginx 模板目录
	cfg.Nginx.TemplatesDir = app.FindTemplatesDir(cfg.Nginx.TemplatesDir)

	// 加载 Nginx 模板
	if err := nginx.InitTemplates(cfg.Nginx.TemplatesDir); err != nil {
		slog.Error("加载 Nginx 模板失败", "error", err)
		os.Exit(1)
	}

	// 初始化 Nginx 路径校验器的白名单
	nginx.SetAllowedRootPrefixes(cfg.Nginx.AllowedRootPrefixes)
	nginx.SetAllowedLogPrefixes(cfg.Nginx.AllowedLogPrefixes)

	// 创建 API 服务器（注入数据库连接）
	server, err := api.NewServer(cfg, database)
	if err != nil {
		slog.Error("创建 API 服务器失败", "error", err)
		os.Exit(1)
	}

	// 配置 HTTP 服务器
	// ReadTimeout 使用 upload_timeout 和 ReadTimeout 中的较大值
	// 因为上传路由需要更长的读超时来接收大文件请求体
	readTimeout := app.ParseDurationOrDefault(cfg.API.ReadTimeout, 15*time.Second)
	uploadTimeout := app.ParseDurationOrDefault(cfg.API.UploadTimeout, 300*time.Second)
	if uploadTimeout > readTimeout {
		readTimeout = uploadTimeout
	}
	httpServer := &http.Server{
		Addr:         cfg.API.Listen,
		Handler:      server.Handler(),
		ReadTimeout:  readTimeout,
		WriteTimeout: app.ParseDurationOrDefault(cfg.API.WriteTimeout, 30*time.Second),
		IdleTimeout:  app.ParseDurationOrDefault(cfg.API.IdleTimeout, 60*time.Second),
	}

	var certPath, keyPath string

	if cfg.API.TLS.Enabled {
		var err error
		certPath, keyPath, err = app.EnsureAPICertificate(&cfg.API.TLS, cfg.DataDir)
		if err != nil {
			slog.Error("TLS 证书准备失败", "error", err)
			os.Exit(1)
		}
		httpServer.TLSConfig = app.NewAPITLSConfig()

		if cfg.API.TLS.Cert == "" && cfg.API.TLS.Key == "" {
			slog.Info("使用自动生成的自签名证书（浏览器会显示安全警告，建议替换为正式证书）", "cert", certPath)
		}

		slog.Info("TLS 已启用", "cert", certPath, "key", keyPath)
	} else {
		slog.Warn("TLS 已禁用，API 数据以明文传输，建议启用 TLS")
	}

	// 优雅关闭：监听系统信号
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		slog.Info("收到关闭信号，开始优雅关闭", "signal", sig.String())

		shutdownTimeout := app.ParseDurationOrDefault(cfg.API.ShutdownTimeout, 10*time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := httpServer.Shutdown(ctx); err != nil {
			slog.Error("服务器关闭出错", "error", err)
		}
	}()

	// 启动服务
	if cfg.API.TLS.Enabled {
		slog.Info("API 服务已启动 (HTTPS)", "addr", cfg.API.Listen)
		if err := httpServer.ListenAndServeTLS(certPath, keyPath); err != nil && err != http.ErrServerClosed {
			slog.Error("API 服务异常退出", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Info("API 服务已启动", "addr", cfg.API.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("API 服务异常退出", "error", err)
			os.Exit(1)
		}
	}

	slog.Info("nxpanel-api 已停止")
	fmt.Println("nxpanel-api 已停止")
}

func panelAccessURL(listen string, tlsEnabled bool, loginPath string) string {
	scheme := "http"
	if tlsEnabled {
		scheme = "https"
	}

	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return scheme + "://" + strings.TrimRight(listen, "/") + loginPath
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "<server-ip>"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + loginPath
}
