package api

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/accessanalysis"
	"github.com/luoye663/nxpanel/internal/accesslimit"
	"github.com/luoye663/nxpanel/internal/acme"
	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/auth"
	"github.com/luoye663/nxpanel/internal/captcha"
	"github.com/luoye663/nxpanel/internal/config"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/hotlink"
	"github.com/luoye663/nxpanel/internal/logs"
	"github.com/luoye663/nxpanel/internal/nginxconf"
	"github.com/luoye663/nxpanel/internal/proxy"
	"github.com/luoye663/nxpanel/internal/rewrite"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
	"github.com/luoye663/nxpanel/internal/settings"
	"github.com/luoye663/nxpanel/internal/sitebackup"
	"github.com/luoye663/nxpanel/internal/sites"
	"github.com/luoye663/nxpanel/internal/sse"
	"github.com/luoye663/nxpanel/internal/ssl"
	"github.com/luoye663/nxpanel/internal/systemmetrics"
	"github.com/luoye663/nxpanel/internal/twofa"
	"github.com/luoye663/nxpanel/internal/upgrade"
)

type repos struct {
	site            *repo.SiteRepo
	proxy           *repo.ProxyRepo
	ssl             *repo.SSLRepo
	rewrite         *repo.RewriteRepo
	rewriteTemplate *repo.RewriteTemplateRepo
	certificate     *repo.CertificateRepo
	authAccount     *repo.AuthAccountRepo
	authRule        *repo.AuthRuleRepo
	denyRule        *repo.DenyRuleRepo
	ipWhitelistRule *repo.IPWhitelistRuleRepo
	hotlinkRule     *repo.HotlinkRuleRepo
	backupSchedule  *repo.SiteBackupScheduleRepo
	siteBackup      *repo.SiteBackupRepo
	acme            *repo.ACMERepo
	admin           *repo.AdminRepo
	loginAudit      *repo.LoginAuditRepo
	accessAnalysis  *accessanalysis.Repo
	scheduledTask   *scheduledtask.Repo
}

func newServerBase(cfg *app.Config, db *sql.DB) *Server {
	rootCtx, rootCancel := context.WithCancel(context.Background())
	if err := app.ValidateLoginPath(cfg.API.LoginPath); err != nil {
		cfg.API.LoginPath = app.GenerateLoginPath()
	}
	sessionDuration := app.ParseDurationOrDefault(cfg.API.SessionDuration, 24*time.Hour)
	rateLimitWindow := app.ParseDurationOrDefault(cfg.API.RateLimit.Window, 15*time.Minute)
	maxFailures := cfg.API.RateLimit.MaxFailures
	if maxFailures <= 0 {
		maxFailures = 5
	}
	sseMaxConnections := cfg.API.SSEMaxConnections
	if sseMaxConnections <= 0 {
		sseMaxConnections = 64
	}

	authSvc := auth.NewAuthService(db, sessionDuration,
		cfg.API.MaxSessions,
		cfg.API.BindSessionIP,
		cfg.API.BindSessionUA,
	)
	loginProtection := middleware.NewLoginProtectionWithContext(rootCtx, middleware.LoginProtectionConfig{
		IPMaxFailures:      maxFailures,
		AccountMaxFailures: cfg.API.RateLimit.AccountMaxFailures,
		GlobalMaxFailures:  cfg.API.RateLimit.GlobalMaxFailures,
		Window:             rateLimitWindow,
	})
	setupLimiter := middleware.NewLoginRateLimiterWithContext(rootCtx, 10, 1*time.Hour)
	// 已登录后的敏感操作使用独立限流器，按 session 临时锁定，避免影响普通登录 IP 限流。
	sensitiveActionLimiter := middleware.NewLoginRateLimiterWithContext(rootCtx, maxFailures, rateLimitWindow)
	captchaSvc := captcha.NewService(
		cfg.API.Captcha.Provider,
		cfg.API.Captcha.SecretKey,
		cfg.API.Captcha.SiteKey,
		cfg.API.Captcha.TriggerAfterFailures,
		cfg.API.Captcha.MaxConcurrent,
	)
	repos := newRepos(db)
	adminExists, _ := authSvc.AdminExists()

	server := &Server{
		cfg:                    cfg,
		db:                     db,
		authSvc:                authSvc,
		loginProtection:        loginProtection,
		setupLimiter:           setupLimiter,
		sensitiveActionLimiter: sensitiveActionLimiter,
		captchaSvc:             captchaSvc,
		twofaSvc:               twofa.NewServiceWithContext(rootCtx, repos.admin, cfg.API.TwoFA.TempTokenMaxPerAccount, cfg.API.TwoFA.TempTokenMaxTotal),
		loginAuditRepo:         repos.loginAudit,
		agentClient:            newAgentClient(cfg),
		opRepo:                 repo.NewOperationRepo(db),
		settingsRepo:           repo.NewSettingsRepo(db),
		backupRepo:             repo.NewBackupRepo(db),
		sslRepo:                repo.NewSSLRepo(db),
		proxyRepo:              repo.NewProxyRepo(db),
		metricsSvc:             systemmetrics.NewService(rootCtx, app.ParseDurationOrDefault(cfg.API.SystemMetricsInterval, 2*time.Second)),
		sseHub:                 sse.NewHub(),
		sseSlots:               make(chan struct{}, sseMaxConnections),
		router:                 chi.NewRouter(),
		rootCtx:                rootCtx,
		rootCancel:             rootCancel,
	}
	server.setGateState(cfg.API.LoginPath, cfg.API.PublicHealth)
	server.SetNeedsSetup(!adminExists)
	return server
}

func newRepos(db *sql.DB) repos {
	return repos{
		site:            repo.NewSiteRepo(db),
		proxy:           repo.NewProxyRepo(db),
		ssl:             repo.NewSSLRepo(db),
		rewrite:         repo.NewRewriteRepo(db),
		rewriteTemplate: repo.NewRewriteTemplateRepo(db),
		certificate:     repo.NewCertificateRepo(db),
		authAccount:     repo.NewAuthAccountRepo(db),
		authRule:        repo.NewAuthRuleRepo(db),
		denyRule:        repo.NewDenyRuleRepo(db),
		ipWhitelistRule: repo.NewIPWhitelistRuleRepo(db),
		hotlinkRule:     repo.NewHotlinkRuleRepo(db),
		backupSchedule:  repo.NewSiteBackupScheduleRepo(db),
		siteBackup:      repo.NewSiteBackupRepo(db),
		acme:            repo.NewACMERepo(db),
		admin:           repo.NewAdminRepo(db),
		loginAudit:      repo.NewLoginAuditRepo(db),
		accessAnalysis:  accessanalysis.NewRepo(db),
		scheduledTask:   scheduledtask.NewRepo(db),
	}
}

func newAgentClient(cfg *app.Config) *agentclient.Client {
	if cfg.Agent.SocketPath == "" {
		return nil
	}
	return agentclient.New(
		cfg.Agent.SocketPath,
		cfg.Agent.Token,
		app.ParseDurationOrDefault(cfg.Agent.ClientTimeout, 30*time.Second),
		app.ParseDurationOrDefault(cfg.Agent.DialTimeout, 3*time.Second),
		app.ParseDurationOrDefault(cfg.Agent.IdleConnTimeout, 30*time.Second),
	)
}

func (s *Server) initScheduledTaskCenter(taskRepo *scheduledtask.Repo) error {
	registry := scheduledtask.NewRegistry()
	runner := scheduledtask.NewRunner(taskRepo, registry, app.NewID("runner"), 2)
	engine := scheduledtask.NewEngineWithConfig(s.rootCtx, taskRepo, runner, scheduledtask.EngineConfig{
		ReconcileInterval: s.cfg.API.AsyncJobs.ScheduledTaskReconcileInterval(),
		DispatchWorkers:   s.cfg.API.AsyncJobs.ScheduledWorkers,
		DispatchQueueSize: s.cfg.API.AsyncJobs.ScheduledQueueSize,
	})
	s.scheduledTaskSvc = scheduledtask.NewService(s.rootCtx, taskRepo, registry, runner, engine, s.cfg.API.AsyncJobs.ManualQueueSize, s.cfg.API.AsyncJobs.ManualWorkers)
	s.scheduledTaskEngine = engine
	return nil
}

func (s *Server) initAgentBackedServices(r repos) error {
	if s.agentClient == nil {
		return nil
	}

	s.siteSvc = sites.NewService(
		s.db, r.site, r.proxy, r.ssl, r.rewrite,
		s.opRepo, s.agentClient, s.cfg,
	)
	s.proxySvc = proxy.NewService(r.site, r.proxy, r.authAccount, s.opRepo, s.agentClient, s.cfg)
	sslAgent := &sslAgentAdapter{client: s.agentClient}
	s.sslSvc = ssl.NewService(r.site, r.ssl, r.certificate, s.opRepo, sslAgent, s.cfg)
	s.rewriteSvc = rewrite.NewService(r.site, r.rewrite, s.opRepo, s.agentClient, r.rewriteTemplate)
	s.accessLimitSvc = accesslimit.NewService(r.site, r.authAccount, r.authRule, r.denyRule, r.ipWhitelistRule, r.proxy, s.opRepo, s.agentClient, s.agentClient, s.cfg.Nginx.PanelDir)
	s.hotlinkSvc = hotlink.NewService(r.site, r.hotlinkRule, s.opRepo, s.agentClient, s.cfg.Nginx.PanelDir)
	s.configSvc = config.NewService(r.site, r.proxy, r.ssl, s.opRepo, s.agentClient)
	s.settingsSvc = settings.NewService(
		s.settingsRepo, r.site, r.certificate, r.ssl, r.proxy, s.opRepo, s.agentClient, s.cfg,
	)
	s.settingsSvc.SetConfigReloader(s)
	if err := s.settingsSvc.AttachScheduledTasks(s.scheduledTaskSvc); err != nil {
		return fmt.Errorf("注册 Nginx 日志切割计划任务失败: %w", err)
	}
	if err := s.settingsSvc.EnsureNginxLogRotationSystemTask(s.rootCtx); err != nil {
		return fmt.Errorf("创建 Nginx 日志切割系统任务失败: %w", err)
	}
	s.siteSvc.SetSettingsProvider(s.settingsSvc)
	s.logsSvc = logs.NewService(r.site, s.opRepo, s.agentClient)
	s.accessAnalysisSvc = accessanalysis.NewService(s.rootCtx, r.site, r.accessAnalysis, s.opRepo, s.agentClient)
	if err := s.accessAnalysisSvc.RecoverStaleJobs(s.rootCtx); err != nil {
		return fmt.Errorf("恢复遗留访问分析任务失败: %w", err)
	}
	s.accessAnalysisSvc.SetTaskLogDir(s.cfg.TaskLogDir())
	if err := s.accessAnalysisSvc.AttachScheduledTasks(s.scheduledTaskSvc); err != nil {
		return fmt.Errorf("注册访问分析计划任务失败: %w", err)
	}
	if err := s.accessAnalysisSvc.MigrateSettingsToTasks(s.rootCtx); err != nil {
		return fmt.Errorf("迁移访问分析计划任务失败: %w", err)
	}
	s.nginxconfSvc = nginxconf.NewService(s.agentClient, &nginxConfigRefresher{cfg: s.cfg}, s.opRepo)
	s.siteBackupSvc = sitebackup.NewService(s.rootCtx, s.cfg.API.AsyncJobs.BackupMaxConcurrent, r.site, r.siteBackup, r.backupSchedule, r.ssl, s.opRepo, s.agentClient, s.cfg.Nginx.PanelDir, s.sseHub)
	s.siteBackupSvc.SetTaskLogDir(s.cfg.TaskLogDir())
	if err := s.siteBackupSvc.AttachScheduledTasks(s.scheduledTaskSvc); err != nil {
		return fmt.Errorf("注册站点备份计划任务失败: %w", err)
	}
	if err := s.siteBackupSvc.MigrateSchedulesToTasks(s.rootCtx); err != nil {
		return fmt.Errorf("迁移站点备份计划任务失败: %w", err)
	}

	s.acmeSvc = acme.NewService(
		s.rootCtx, s.cfg.API.AsyncJobs.ACMEMaxConcurrent,
		r.site, r.ssl, r.certificate, r.acme, s.opRepo,
		&acmeAgentAdapter{client: s.agentClient},
		s.sslSvc,
		s.sseHub, s.cfg,
	)
	if err := s.acmeSvc.AttachScheduledTasks(s.scheduledTaskSvc); err != nil {
		return fmt.Errorf("注册 SSL 自动续签计划任务失败: %w", err)
	}
	if err := s.acmeSvc.EnsureRenewalSystemTask(s.rootCtx, s.scheduledTaskSvc); err != nil {
		return fmt.Errorf("创建 SSL 自动续签系统任务失败: %w", err)
	}

	return nil
}

func (s *Server) startRuntimeServices() {
	s.backgroundWG.Add(3)
	go func() {
		defer s.backgroundWG.Done()
		s.sessionCleanup(s.rootCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		s.sseCleanup(s.rootCtx)
	}()
	go func() {
		defer s.backgroundWG.Done()
		policy := s.cfg.Database.Retention.Policy()
		maintenanceLoop(s.rootCtx, policy.CleanupInterval, s.pruneRetainedData)
	}()

	s.upgradeSvc = upgrade.NewService(s.cfg.Upgrade)
	s.upgradeSvc.Start(s.rootCtx)
}
