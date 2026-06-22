package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/luoye663/nxpanel/internal/api/middleware"
	"github.com/luoye663/nxpanel/web"
)

func (s *Server) setupRoutes() {
	s.router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		if !s.PublicHealthEnabled() {
			http.NotFound(w, r)
			return
		}
		s.handleHealth(w, r)
	})

	s.router.Route("/api/v1/{gateSecret}", func(r chi.Router) {
		r.Use(middleware.GateSecretValidator(s.GateSecretToken))
		// Setup（无需认证，有独立限流）
		r.With(middleware.LoginRateLimitMiddleware(s.setupLimiter)).Post("/setup/admin", s.handleSetupAdmin)

		// Auth — 登录（限流）
		r.With(middleware.LoginRateLimitMiddleware(s.limiter)).Post("/auth/login", s.handleLogin)

		// Auth — 2FA 验证（公开，但需要 temp_token，限流）
		r.With(middleware.LoginRateLimitMiddleware(s.limiter)).Post("/auth/login/2fa", s.handleLogin2FA)
		r.With(middleware.LoginRateLimitMiddleware(s.limiter)).Post("/auth/login/recover", s.handleLoginRecover)

		// Auth — 状态查询
		r.Get("/auth/me", s.handleMe)
		r.Get("/auth/captcha-config", s.handleCaptchaConfig)
		r.Post("/auth/logout", s.handleLogout)

		// 以下路由需要认证
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth)

			// 2FA 管理
			r.Get("/auth/2fa/status", s.handle2FAStatus)
			r.Post("/auth/2fa/setup", s.handle2FASetup)
			r.Post("/auth/2fa/enable", s.handle2FAEnable)
			r.Post("/auth/2fa/disable", s.handle2FADisable)
			r.Post("/auth/2fa/regenerate-codes", s.handle2FARegenerateRecoveryCodes)

			// 密码管理
			r.Post("/auth/change-password", s.handleChangePassword)

			// Login Audit
			r.Get("/auth/login-audit", s.handleLoginAuditList)
			r.Delete("/auth/login-audit", s.handleLoginAuditClear)

			// System / Agent / Nginx
			r.Get("/system/overview", s.handleSystemOverview)
			r.Get("/system/upgrade", s.handleUpgradeCheck)
			r.Post("/system/upgrade/check", s.handleUpgradeCheckTrigger)
			r.Get("/system/metrics/stream", s.handleSystemMetricsStream)
			r.Post("/nginx/detect", s.handleNginxDetect)
			r.Post("/nginx/include/ensure", s.handleNginxIncludeEnsure)
			r.Post("/nginx/test", s.handleNginxTest)
			r.Post("/nginx/reload", s.handleNginxReload)

			r.Get("/nginx/conf", s.handleNginxConfGet)
			r.Put("/nginx/conf", s.handleNginxConfSave)
			r.Get("/nginx/parameters", s.handleNginxParametersGet)
			r.Put("/nginx/parameters", s.handleNginxParametersSave)

			// Sites
			r.Get("/sites", s.handleSiteList)
			r.Post("/sites", s.handleSiteCreate)
			r.Get("/sites/{site_id}", s.handleSiteDetail)
			r.Put("/sites/{site_id}", s.handleSiteUpdate)
			r.Put("/sites/{site_id}/document", s.handleSiteDocumentUpdate)
			r.Delete("/sites/{site_id}", s.handleSiteDelete)
			r.Post("/sites/{site_id}/enable", s.handleSiteEnable)
			r.Post("/sites/{site_id}/disable", s.handleSiteDisable)

			// Proxy
			r.Get("/sites/{site_id}/proxy", s.handleProxyList)
			r.Post("/sites/{site_id}/proxy", s.handleProxyCreate)
			r.Get("/sites/{site_id}/proxy/{proxy_id}", s.handleProxyGet)
			r.Put("/sites/{site_id}/proxy/{proxy_id}", s.handleProxyUpdate)
			r.Delete("/sites/{site_id}/proxy/{proxy_id}", s.handleProxyDelete)

			// SSL
			r.Get("/sites/{site_id}/ssl", s.handleSSLGet)
			r.Put("/sites/{site_id}/ssl/manual-pem", s.handleSSLManualPEM)
			r.Put("/sites/{site_id}/ssl/existing-files", s.handleSSLExistingFiles)
			r.Delete("/sites/{site_id}/ssl", s.handleSSLDisable)
			r.Get("/sites/{site_id}/ssl/content", s.handleSSLContent)
			r.Get("/sites/{site_id}/ssl/download", s.handleSSLDownload)

			// Rewrite
			r.Get("/rewrite/templates", s.handleRewriteTemplateList)
			r.Post("/rewrite/templates", s.handleRewriteTemplateCreate)
			r.Put("/rewrite/templates/{template_id}", s.handleRewriteTemplateUpdate)
			r.Delete("/rewrite/templates/{template_id}", s.handleRewriteTemplateDelete)
			r.Post("/rewrite/templates/preview", s.handleRewriteTemplatePreview)
			r.Get("/sites/{site_id}/rewrite", s.handleRewriteGet)
			r.Put("/sites/{site_id}/rewrite", s.handleRewriteUpdate)
			r.Post("/sites/{site_id}/rewrite/apply-template", s.handleRewriteApplyTemplate)

			// Access Limit
			r.Get("/sites/{site_id}/auth-rules", s.handleAuthRuleList)
			r.Post("/sites/{site_id}/auth-rules", s.handleAuthRuleCreate)
			r.Put("/sites/{site_id}/auth-rules/{rule_id}", s.handleAuthRuleUpdate)
			r.Delete("/sites/{site_id}/auth-rules/{rule_id}", s.handleAuthRuleDelete)
			r.Get("/sites/{site_id}/deny-rules", s.handleDenyRuleList)
			r.Post("/sites/{site_id}/deny-rules", s.handleDenyRuleCreate)
			r.Put("/sites/{site_id}/deny-rules/{rule_id}", s.handleDenyRuleUpdate)
			r.Delete("/sites/{site_id}/deny-rules/{rule_id}", s.handleDenyRuleDelete)
			r.Get("/sites/{site_id}/ip-limit-rules", s.handleIPLimitRuleList)
			r.Post("/sites/{site_id}/ip-limit-rules", s.handleIPLimitRuleCreate)
			r.Put("/sites/{site_id}/ip-limit-rules/{rule_id}", s.handleIPLimitRuleUpdate)
			r.Delete("/sites/{site_id}/ip-limit-rules/{rule_id}", s.handleIPLimitRuleDelete)
			r.Get("/sites/{site_id}/hotlink-rules", s.handleHotlinkRuleList)
			r.Post("/sites/{site_id}/hotlink-rules", s.handleHotlinkRuleCreate)
			r.Put("/sites/{site_id}/hotlink-rules/{rule_id}", s.handleHotlinkRuleUpdate)
			r.Delete("/sites/{site_id}/hotlink-rules/{rule_id}", s.handleHotlinkRuleDelete)

			// Config
			r.Get("/sites/{site_id}/config", s.handleConfigGet)
			r.Put("/sites/{site_id}/config", s.handleConfigSave)
			r.Post("/sites/{site_id}/config/migrate-markers", s.handleConfigMigrateMarkers)

			// Logs
			r.Get("/sites/{site_id}/logs", s.handleLogGet)
			r.Get("/sites/{site_id}/logs/download", s.handleLogDownload)
			r.Get("/sites/{site_id}/logs/search", s.handleLogSearch)
			r.Get("/sites/{site_id}/logs/stream", s.handleLogStream)
			r.Get("/sites/{site_id}/logs/rotated", s.handleRotatedLogList)
			r.Get("/sites/{site_id}/logs/rotated/tail", s.handleRotatedLogTail)
			r.Delete("/sites/{site_id}/logs/rotated", s.handleRotatedLogDelete)
			r.Post("/sites/{site_id}/logs/truncate", s.handleLogTruncate)

			// Site Backups
			r.Get("/sites/{site_id}/backups", s.handleSiteBackupList)
			r.Post("/sites/{site_id}/backups", s.handleSiteBackupCreate)
			r.Get("/sites/{site_id}/backups/schedule", s.handleSiteBackupScheduleGet)
			r.Put("/sites/{site_id}/backups/schedule", s.handleSiteBackupScheduleSave)
			r.Get("/sites/{site_id}/backups/tasks/{task_id}/stream", s.handleSiteBackupTaskStream)
			r.Get("/sites/{site_id}/backups/{backup_id}/download", s.handleSiteBackupDownload)
			r.Post("/sites/{site_id}/backups/{backup_id}/restore", s.handleSiteBackupRestore)
			r.Delete("/sites/{site_id}/backups/{backup_id}", s.handleSiteBackupDelete)

			// Access Analysis
			r.Get("/sites/{site_id}/access-analysis/summary", s.handleAccessAnalysisSummary)
			r.Post("/sites/{site_id}/access-analysis/scan", s.handleAccessAnalysisScan)
			r.Get("/sites/{site_id}/access-analysis/jobs", s.handleAccessAnalysisJobs)
			r.Get("/sites/{site_id}/access-analysis/paths", s.handleAccessAnalysisPaths)
			r.Get("/sites/{site_id}/access-analysis/ips", s.handleAccessAnalysisIPs)
			r.Get("/sites/{site_id}/access-analysis/entries", s.handleAccessAnalysisEntries)
			r.Get("/sites/{site_id}/access-analysis/anomalies", s.handleAccessAnalysisAnomalies)
			r.Get("/sites/{site_id}/access-analysis/export", s.handleAccessAnalysisExport)
			r.Get("/sites/{site_id}/access-analysis/settings", s.handleAccessAnalysisSettingsGet)
			r.Put("/sites/{site_id}/access-analysis/settings", s.handleAccessAnalysisSettingsPut)
			r.Post("/sites/{site_id}/access-analysis/format/detect", s.handleAccessAnalysisFormatDetect)
			r.Post("/sites/{site_id}/access-analysis/format/test", s.handleAccessAnalysisFormatTest)
			r.Post("/sites/{site_id}/access-analysis/format/optimize", s.handleAccessAnalysisFormatOptimize)

			// Discovered Servers
			r.Post("/sites/import-scan", s.handleImportScan)
			r.Post("/sites/import", s.handleSiteImport)

			// Certificates
			r.Get("/certificates", s.handleCertificateList)
			r.Post("/certificates", s.handleCertificateCreate)
			r.Delete("/certificates/{cert_id}", s.handleCertificateDelete)
			r.Post("/certificates/{cert_id}/deploy", s.handleCertificateDeploy)

			// ACME
			r.Post("/acme/apply", s.handleACMEApply)
			r.Get("/acme/orders/{order_id}/log", s.handleACMEOrderLog)
			r.Post("/acme/orders/{order_id}/renew", s.handleACMEOrderRenew)
			r.Delete("/acme/orders/{order_id}", s.handleACMEOrderDelete)
			r.Get("/acme/orders/{order_id}/download", s.handleACMEOrderDownload)
			r.Post("/acme/orders/{order_id}/deploy", s.handleACMEOrderDeploy)
			r.Put("/acme/orders/{order_id}/auto-renew", s.handleACMEOrderAutoRenew)
			r.Post("/acme/orders/{order_id}/force-obtain", s.handleACMEOrderForceObtain)
			r.Get("/sites/{site_id}/acme/orders", s.handleACMEOrderList)
			r.Get("/acme/emails", s.handleACMEEmailList)
			r.Post("/acme/emails", s.handleACMEEmailSave)
			r.Delete("/acme/emails/{email}", s.handleACMEEmailDelete)

			// Settings
			r.Get("/settings/default-pages", s.handleSettingsDefaultPagesGet)
			r.Put("/settings/default-pages", s.handleSettingsDefaultPagesUpdate)
			r.Get("/settings/default-site", s.handleSettingsDefaultSiteGet)
			r.Put("/settings/default-site", s.handleSettingsDefaultSiteUpdate)
			r.Get("/settings/https-hijack", s.handleSettingsHTTPSHijackGet)
			r.Put("/settings/https-hijack", s.handleSettingsHTTPSHijackUpdate)
			r.Get("/settings/log-rotation", s.handleSettingsLogRotateGet)
			r.Put("/settings/log-rotation", s.handleSettingsLogRotateUpdate)
			r.Get("/settings/security", s.handleSecuritySettingsGet)
			r.Put("/settings/security", s.handleSecuritySettingsUpdate)

			// Operations
			r.Get("/operations", s.handleOperationList)
			r.Get("/operations/{operation_id}", s.handleOperationDetail)
			r.Delete("/operations", s.handleOperationClear)

			// Service Logs (运行日志)
			r.Get("/system/service-logs", s.handleServiceLogTail)
			r.Get("/system/service-logs/stream", s.handleServiceLogStream)
			r.Delete("/system/service-logs", s.handleServiceLogClear)

			// Task Logs (计划任务日志)
			r.Get("/task-logs/types", s.handleTaskLogTypes)
			r.Get("/task-logs", s.handleTaskLogTail)
			r.Delete("/task-logs", s.handleTaskLogClear)

			// Scheduled Tasks（统一计划任务中心）
			r.Get("/scheduled-tasks/definitions", s.handleScheduledTaskDefinitions)
			r.Get("/scheduled-tasks", s.handleScheduledTaskList)
			r.Post("/scheduled-tasks", s.handleScheduledTaskCreate)
			r.Put("/scheduled-tasks/{task_id}", s.handleScheduledTaskUpdate)
			r.Post("/scheduled-tasks/{task_id}/toggle", s.handleScheduledTaskToggle)
			r.Post("/scheduled-tasks/{task_id}/run", s.handleScheduledTaskRunNow)
			r.Get("/scheduled-tasks/{task_id}/runs", s.handleScheduledTaskRunList)
			r.Delete("/scheduled-tasks/{task_id}", s.handleScheduledTaskDelete)

			// Files — 全局文件管理
			r.Get("/files/roots", s.handleGlobalFilesRoots)
			r.Get("/files/list", s.handleGlobalFilesList)
			r.Get("/files/read", s.handleGlobalFilesRead)
			r.Post("/files/write", s.handleGlobalFilesWrite)
			r.Post("/files/remove", s.handleGlobalFilesRemove)
			r.Post("/files/mkdir", s.handleGlobalFilesMkdir)
			r.Post("/files/move", s.handleGlobalFilesMove)
			r.Post("/files/copy", s.handleGlobalFilesCopy)
			r.Post("/files/chmod", s.handleGlobalFilesChmod)
			r.Post("/files/chown", s.handleGlobalFilesChown)
			r.Post("/files/compress", s.handleGlobalFilesCompress)
			r.Post("/files/extract", s.handleGlobalFilesExtract)
			r.Get("/files/download", s.handleGlobalFilesDownload)
			r.Get("/files/archive", s.handleGlobalFilesArchive)
			r.Post("/files/upload", s.handleGlobalFilesUpload)

			// Files — 站点文件管理
			r.Get("/sites/{site_id}/files", s.handleFilesList)
			r.Get("/sites/{site_id}/files/read", s.handleFilesRead)
			r.Post("/sites/{site_id}/files/write", s.handleFilesWrite)
			r.Post("/sites/{site_id}/files/remove", s.handleFilesRemove)
			r.Post("/sites/{site_id}/files/mkdir", s.handleFilesMkdir)
			r.Post("/sites/{site_id}/files/move", s.handleFilesMove)
			r.Post("/sites/{site_id}/files/copy", s.handleFilesCopy)
			r.Post("/sites/{site_id}/files/chmod", s.handleFilesChmod)
			r.Post("/sites/{site_id}/files/chown", s.handleFilesChown)
			r.Post("/sites/{site_id}/files/compress", s.handleFilesCompress)
			r.Post("/sites/{site_id}/files/extract", s.handleFilesExtract)
			r.Get("/sites/{site_id}/files/download", s.handleFilesDownload)
			r.Get("/sites/{site_id}/files/archive", s.handleFilesArchive)
			r.Post("/sites/{site_id}/files/upload", s.handleFilesUpload)
		})
	})

	s.setupStaticFiles()
}

func (s *Server) setupStaticFiles() {
	webDir, err := web.StaticFS()
	if err != nil {
		slog.Error("初始化前端目录失败", "error", err)
		return
	}

	fileServer := http.FileServer(http.Dir(webDir))
	var indexOnce sync.Once
	var indexHTML []byte
	var indexErr error
	loadIndex := func() ([]byte, error) {
		indexOnce.Do(func() {
			indexHTML, indexErr = os.ReadFile(filepath.Join(webDir, "index.html"))
		})
		return indexHTML, indexErr
	}

	s.router.Handle("/assets/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	}))

	s.router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		if strings.HasPrefix(path, "/api/") || (path == "/health" && !s.PublicHealthEnabled()) {
			http.NotFound(w, r)
			return
		}

		fullPath := filepath.Join(webDir, strings.TrimPrefix(path, "/"))
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}

		if middleware.GetSession(r.Context()) == nil && path != s.CurrentLoginPath() {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		serveInjectedIndex(w, loadIndex, s.CurrentLoginPath())
	})
}

func serveInjectedIndex(w http.ResponseWriter, loadIndex func() ([]byte, error), loginPath string) {
	indexHTML, err := loadIndex()
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	// 只在返回 SPA 时注入隐藏入口，匿名未命中路径不会拿到该值。
	encodedPath, _ := json.Marshal(loginPath)
	script := []byte(`<script>window.__NX_GATE_PATH__=` + string(encodedPath) + `;</script>`)
	out := indexHTML
	if idx := bytes.Index(indexHTML, []byte("</head>")); idx >= 0 {
		out = make([]byte, 0, len(indexHTML)+len(script))
		out = append(out, indexHTML[:idx]...)
		out = append(out, script...)
		out = append(out, indexHTML[idx:]...)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}
