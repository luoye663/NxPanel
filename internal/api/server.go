package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"

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

type Server struct {
	cfg                    *app.Config
	db                     *sql.DB
	authSvc                *auth.AuthService
	limiter                *middleware.LoginRateLimiter
	setupLimiter           *middleware.LoginRateLimiter
	sensitiveActionLimiter *middleware.LoginRateLimiter
	captchaSvc             *captcha.Service
	twofaSvc               *twofa.Service
	loginAuditRepo         *repo.LoginAuditRepo
	agentClient            *agentclient.Client
	opRepo                 *repo.OperationRepo
	settingsRepo           *repo.SettingsRepo
	siteSvc                *sites.Service
	proxySvc               *proxy.Service
	sslSvc                 *ssl.Service
	rewriteSvc             *rewrite.Service
	accessLimitSvc         *accesslimit.Service
	hotlinkSvc             *hotlink.Service
	configSvc              *config.Service
	settingsSvc            *settings.Service
	nginxconfSvc           *nginxconf.Service
	logsSvc                *logs.Service
	siteBackupSvc          *sitebackup.Service
	accessAnalysisSvc      *accessanalysis.Service
	backupRepo             *repo.BackupRepo
	sslRepo                *repo.SSLRepo
	proxyRepo              *repo.ProxyRepo
	sseHub                 *sse.Hub
	metricsSvc             *systemmetrics.Service
	acmeSvc                *acme.Service
	scheduledTaskSvc       *scheduledtask.Service
	scheduledTaskEngine    *scheduledtask.Engine
	upgradeSvc             *upgrade.Service
	router                 *chi.Mux
	cancelCleanup          context.CancelFunc
	gateSecret             atomic.Value
	loginPath              atomic.Value
	needsSetup             atomic.Bool
	publicHealth           atomic.Bool
}

func NewServer(cfg *app.Config, db *sql.DB) (*Server, error) {
	s := newServerBase(cfg, db)
	r := newRepos(db)
	if err := s.initScheduledTaskCenter(r.scheduledTask); err != nil {
		return nil, err
	}
	if err := s.initAgentBackedServices(r); err != nil {
		return nil, err
	}
	s.startRuntimeServices()

	s.setupMiddleware()
	s.setupRoutes()

	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.router
}

func (s *Server) Close() {
	if s.cancelCleanup != nil {
		s.cancelCleanup()
	}
	if s.limiter != nil {
		s.limiter.Stop()
	}
	if s.setupLimiter != nil {
		s.setupLimiter.Stop()
	}
	if s.sensitiveActionLimiter != nil {
		s.sensitiveActionLimiter.Stop()
	}
	if s.twofaSvc != nil {
		s.twofaSvc.Stop()
	}
	if s.metricsSvc != nil {
		s.metricsSvc.Close()
	}
	if s.scheduledTaskEngine != nil {
		s.scheduledTaskEngine.Stop()
	}
}

func (s *Server) ReloadSecurityConfig(cfg *app.Config) {
	maxFailures := cfg.API.RateLimit.MaxFailures
	if maxFailures <= 0 {
		maxFailures = 5
	}
	window := app.ParseDurationOrDefault(cfg.API.RateLimit.Window, 15*time.Minute)
	if s.limiter != nil {
		s.limiter.ReloadConfig(maxFailures, window)
	}
	if s.sensitiveActionLimiter != nil {
		s.sensitiveActionLimiter.ReloadConfig(maxFailures, window)
	}
	if s.authSvc != nil {
		s.authSvc.ReloadSecurityConfig(cfg.API.MaxSessions, cfg.API.BindSessionIP, cfg.API.BindSessionUA)
	}
	middleware.ReloadTrustedProxies(cfg.API.TrustedProxies)
	if s.captchaSvc != nil {
		s.captchaSvc.ReloadConfig(cfg.API.Captcha.Provider, cfg.API.Captcha.SecretKey, cfg.API.Captcha.SiteKey, cfg.API.Captcha.TriggerAfterFailures)
	}
	s.setGateState(cfg.API.LoginPath, cfg.API.PublicHealth)
	slog.Info("安全配置已热重载")
}

func (s *Server) setGateState(loginPath string, publicHealth bool) {
	s.loginPath.Store(loginPath)
	s.gateSecret.Store(strings.TrimPrefix(loginPath, "/"))
	s.publicHealth.Store(publicHealth)
}

func (s *Server) GateSecretToken() string {
	if v, ok := s.gateSecret.Load().(string); ok {
		return v
	}
	return ""
}

func (s *Server) CurrentLoginPath() string {
	if v, ok := s.loginPath.Load().(string); ok {
		return v
	}
	return s.cfg.API.LoginPath
}

func (s *Server) NeedsSetupCached() bool {
	return s.needsSetup.Load()
}

func (s *Server) SetNeedsSetup(value bool) {
	s.needsSetup.Store(value)
}

func (s *Server) PublicHealthEnabled() bool {
	return s.publicHealth.Load()
}

func (s *Server) sessionCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.authSvc.CleanupExpiredSessions()
			if err != nil {
				slog.Warn("清理过期会话失败", "error", err)
			} else if n > 0 {
				slog.Debug("清理过期会话", "count", n)
			}
		}
	}
}

func (s *Server) setupMiddleware() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.TrustedRealIP(s.cfg.API.TrustedProxies))
	s.router.Use(middleware.SecurityHeaders)
	s.router.Use(middleware.MaxBodySizeExcept(2*1024*1024, "/api/v1/files/upload", "/files/upload"))
	s.router.Use(middleware.Recoverer)
	s.router.Use(chiMiddleware.Logger)
	s.router.Use(middleware.Authenticate(s.authSvc))
	s.router.Use(middleware.CSRFProtect())
}

type sslAgentAdapter struct {
	client *agentclient.Client
}

func (a *sslAgentAdapter) SSLInspect(ctx context.Context, certPEM, keyPEM string) (*ssl.SSLInspectResult, error) {
	resp, err := a.client.SSLInspect(ctx, &agentclient.SSLInspectRequest{
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	})
	if err != nil {
		return nil, err
	}
	return &ssl.SSLInspectResult{
		Subject:    resp.Subject,
		Issuer:     resp.Issuer,
		NotBefore:  resp.NotBefore,
		NotAfter:   resp.NotAfter,
		DNSNames:   resp.DNSNames,
		CertSHA256: resp.CertSHA256,
		KeySHA256:  resp.KeySHA256,
	}, nil
}

func (a *sslAgentAdapter) SSLInspectFiles(ctx context.Context, certPath, keyPath string) (*ssl.SSLInspectResult, error) {
	resp, err := a.client.SSLInspectFiles(ctx, &agentclient.SSLInspectFilesRequest{
		CertPath: certPath,
		KeyPath:  keyPath,
	})
	if err != nil {
		return nil, err
	}
	return &ssl.SSLInspectResult{
		Subject:    resp.Subject,
		Issuer:     resp.Issuer,
		NotBefore:  resp.NotBefore,
		NotAfter:   resp.NotAfter,
		DNSNames:   resp.DNSNames,
		CertSHA256: resp.CertSHA256,
	}, nil
}

func (a *sslAgentAdapter) ApplyTransaction(ctx context.Context, req *ssl.TransactionRequest) error {
	changes := make([]agentclient.FileChangeRequest, len(req.Changes))
	for i, c := range req.Changes {
		changes[i] = agentclient.FileChangeRequest{
			Type:          c.Type,
			Path:          c.Path,
			Target:        c.Target,
			ContentBase64: c.ContentBase64,
			Perm:          c.Perm,
		}
	}
	_, err := a.client.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: req.OperationID,
		Changes:     changes,
		TestNginx:   req.TestNginx,
		ReloadNginx: req.ReloadNginx,
	})
	return err
}

func (a *sslAgentAdapter) ReadFile(ctx context.Context, path string) ([]byte, error) {
	resp, err := a.client.FilesRead(ctx, path)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.ContentBase64)
	if err != nil {
		return nil, fmt.Errorf("解码文件内容失败: %w", err)
	}
	return decoded, nil
}

type acmeAgentAdapter struct {
	client *agentclient.Client
}

func (a *acmeAgentAdapter) ApplyTransaction(ctx context.Context, req *ssl.TransactionRequest) error {
	changes := make([]agentclient.FileChangeRequest, len(req.Changes))
	for i, c := range req.Changes {
		changes[i] = agentclient.FileChangeRequest{
			Type:          c.Type,
			Path:          c.Path,
			Target:        c.Target,
			ContentBase64: c.ContentBase64,
			Perm:          c.Perm,
		}
	}
	_, err := a.client.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: req.OperationID,
		Changes:     changes,
		TestNginx:   req.TestNginx,
		ReloadNginx: req.ReloadNginx,
	})
	return err
}

func (a *acmeAgentAdapter) ReadFile(ctx context.Context, path string) ([]byte, string, error) {
	return a.client.ReadFile(ctx, path)
}

func (a *acmeAgentAdapter) FilesWrite(ctx context.Context, path, contentBase64 string) error {
	return a.client.FilesWrite(ctx, path, contentBase64)
}

func (a *acmeAgentAdapter) FilesRemove(ctx context.Context, paths []string) error {
	return a.client.FilesRemove(ctx, paths)
}

func (a *acmeAgentAdapter) FilesMkdir(ctx context.Context, path string) error {
	return a.client.FilesMkdir(ctx, path)
}

func (a *acmeAgentAdapter) FilesRead(ctx context.Context, path string) ([]byte, error) {
	resp, err := a.client.FilesRead(ctx, path)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(resp.ContentBase64)
	if err != nil {
		return nil, fmt.Errorf("解码文件内容失败: %w", err)
	}
	return decoded, nil
}

type nginxConfigRefresher struct {
	cfg *app.Config
}

func (r *nginxConfigRefresher) GetConfPath() string {
	if r.cfg.Nginx.ConfPath != "" {
		return r.cfg.Nginx.ConfPath
	}
	if err := r.cfg.ReloadFromDisk(); err != nil {
		slog.Debug("从磁盘重读配置失败", "error", err)
	}
	return r.cfg.Nginx.ConfPath
}
