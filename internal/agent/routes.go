// agent 包的路由注册
//
// 内部 RPC 接口，仅通过 Unix Socket 访问。
package agent

// setupRoutes 注册 agent 内部路由
func (s *Server) setupRoutes() {
	// 健康检查
	s.router.Get("/internal/v1/health", s.handleHealth)

	// 文件事务
	s.router.Post("/internal/v1/transactions/apply", s.handleTransactionApply)

	// 站点备份与恢复
	s.router.Post("/internal/v1/backups/site/create", s.handleSiteBackupCreate)
	s.router.Get("/internal/v1/backups/site/download", s.handleSiteBackupDownload)
	s.router.Post("/internal/v1/backups/site/restore", s.handleSiteBackupRestore)
	s.router.Post("/internal/v1/backups/site/remove", s.handleSiteBackupRemove)

	// Nginx 操作
	s.router.Post("/internal/v1/nginx/detect", s.handleNginxDetect)
	s.router.Post("/internal/v1/nginx/ensure-include", s.handleNginxEnsureInclude)
	s.router.Post("/internal/v1/nginx/dump", s.handleNginxDump)
	s.router.Post("/internal/v1/nginx/test", s.handleNginxTest)
	s.router.Post("/internal/v1/nginx/reload", s.handleNginxReload)
	s.router.Post("/internal/v1/nginx/reopen", s.handleNginxReopen)
	s.router.Post("/internal/v1/nginx/logs/rotate-run", s.handleNginxLogRotateRun)

	// 配置操作
	s.router.Post("/internal/v1/config/reload", s.handleConfigReload)
	s.router.Post("/internal/v1/config/write-back", s.handleConfigWriteBack)

	// 日志操作
	s.router.Post("/internal/v1/logs/tail", s.handleLogTail)
	s.router.Post("/internal/v1/logs/truncate", s.handleLogTruncate)
	s.router.Post("/internal/v1/logs/search", s.handleLogSearch)
	s.router.Post("/internal/v1/logs/rotated/list", s.handleRotatedLogList)
	s.router.Post("/internal/v1/logs/rotated/tail", s.handleRotatedLogTail)
	s.router.Post("/internal/v1/logs/rotated/remove", s.handleRotatedLogRemove)
	s.router.Get("/internal/v1/logs/download", s.handleLogDownload)
	s.router.Post("/internal/v1/logs/access-analysis/scan", s.handleAccessAnalysisScan)
	s.router.Post("/internal/v1/logs/access-analysis/format-detect", s.handleAccessAnalysisFormatDetect)

	// 服务运行日志
	s.router.Post("/internal/v1/logs/service/tail", s.handleServiceLogTail)
	s.router.Post("/internal/v1/logs/service/truncate", s.handleServiceLogTruncate)

	// 计划任务日志
	s.router.Post("/internal/v1/logs/tasks/list", s.handleTaskLogList)
	s.router.Post("/internal/v1/logs/tasks/tail", s.handleTaskLogTail)
	s.router.Post("/internal/v1/logs/tasks/truncate", s.handleTaskLogTruncate)

	// SSL 证书检查
	s.router.Post("/internal/v1/ssl/inspect", s.handleSSLInspect)

	// 文件管理
	s.router.Post("/internal/v1/files/list", s.handleFilesList)
	s.router.Post("/internal/v1/files/read", s.handleFilesRead)
	s.router.Post("/internal/v1/files/write", s.handleFilesWrite)
	s.router.Post("/internal/v1/files/remove", s.handleFilesRemove)
	s.router.Post("/internal/v1/files/mkdir", s.handleFilesMkdir)
	s.router.Post("/internal/v1/files/move", s.handleFilesMove)
	s.router.Post("/internal/v1/files/copy", s.handleFilesCopy)
	s.router.Post("/internal/v1/files/upload", s.handleFilesUpload)
	s.router.Post("/internal/v1/files/chmod", s.handleFilesChmod)
	s.router.Post("/internal/v1/files/chown", s.handleFilesChown)
	s.router.Post("/internal/v1/files/compress", s.handleFilesCompress)
	s.router.Post("/internal/v1/files/extract", s.handleFilesExtract)
	s.router.Get("/internal/v1/files/roots", s.handleFilesRoots)
	s.router.Get("/internal/v1/files/download", s.handleFilesDownload)
	s.router.Get("/internal/v1/files/archive", s.handleFilesArchive)
}
