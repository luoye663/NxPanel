package sites

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/parser"
)

type Service struct {
	db          *sql.DB
	siteRepo    *repo.SiteRepo
	proxyRepo   *repo.ProxyRepo
	sslRepo     *repo.SSLRepo
	rewriteRepo *repo.RewriteRepo
	opRepo      *repo.OperationRepo
	agent       *agentclient.Client
	cfg         *app.Config
	settingsSvc SettingsServiceProvider

	// rootsFn 返回 Agent 当前的有效白名单根目录，默认走 agent.FilesRoots。
	// 可在测试中注入以便脱离真实 Agent。
	rootsFn func(ctx context.Context) ([]string, error)
}

type SettingsServiceProvider interface {
	GetDefaultPageTemplate(pageType string) string
}

func NewService(
	db *sql.DB,
	siteRepo *repo.SiteRepo,
	proxyRepo *repo.ProxyRepo,
	sslRepo *repo.SSLRepo,
	rewriteRepo *repo.RewriteRepo,
	opRepo *repo.OperationRepo,
	agent *agentclient.Client,
	cfg *app.Config,
) *Service {
	svc := &Service{
		db:          db,
		siteRepo:    siteRepo,
		proxyRepo:   proxyRepo,
		sslRepo:     sslRepo,
		rewriteRepo: rewriteRepo,
		opRepo:      opRepo,
		agent:       agent,
		cfg:         cfg,
	}
	svc.rootsFn = func(ctx context.Context) ([]string, error) {
		resp, err := svc.agent.FilesRoots(ctx)
		if err != nil {
			return nil, err
		}
		return resp.Roots, nil
	}
	return svc
}

func (svc *Service) SetSettingsProvider(p SettingsServiceProvider) {
	svc.settingsSvc = p
}

func (svc *Service) Create(ctx context.Context, req *CreateSiteRequest, requestID string) (*repo.Site, string, error) {
	if err := ValidateCreate(req); err != nil {
		return nil, "", app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}

	primaryDomain := req.Bindings[0].Domain
	existing, err := svc.siteRepo.GetByPrimaryDomain(primaryDomain)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if existing != nil {
		return nil, "", app.NewAppError(app.ErrConflict,
			fmt.Sprintf("域名 %s 已被站点 %s 使用", primaryDomain, existing.ID), nil)
	}

	siteID := app.NewSiteID()
	panelDir := svc.cfg.Nginx.PanelDir

	domains := make([]string, 0, len(req.Bindings))
	for _, b := range req.Bindings {
		domains = append(domains, b.Domain)
	}
	domainsJSON, _ := json.Marshal(domains)
	bindingsJSON, _ := json.Marshal(req.Bindings)

	status := "disabled"
	if req.EnableAfterCreate {
		status = "enabled"
	}

	site := &repo.Site{
		ID:               siteID,
		PrimaryDomain:    primaryDomain,
		DomainsJSON:      string(domainsJSON),
		BindingsJSON:     string(bindingsJSON),
		Status:           status,
		HTTPPort:         req.Bindings[0].Port,
		HTTPSPort:        443,
		RootPath:         req.RootPath,
		IndexFiles:       req.IndexFiles,
		AccessLogEnabled: req.AccessLogEnabled,
		AccessLogPath:    fmt.Sprintf("%s/%s.access.log", svc.cfg.Nginx.LogDir, primaryDomain),
		ErrorLogPath:     fmt.Sprintf("%s/%s.error.log", svc.cfg.Nginx.LogDir, primaryDomain),
		ConfigPath:       filepath.Join(panelDir, "sites-available", primaryDomain+".conf"),
		EnabledPath:      filepath.Join(panelDir, "sites-enabled", primaryDomain+".conf"),
		RewritePath:      filepath.Join(panelDir, "rewrite", primaryDomain+".conf"),
		AccessLimitPath:  filepath.Join(panelDir, "access-limit", primaryDomain+".conf"),
		HotlinkPath:      filepath.Join(panelDir, "hotlink", primaryDomain+".conf"),
		MarkerVersion:    1,
	}

	serverNames := strings.Join(domains, " ")
	renderData := &nginx.RenderData{
		SiteID:           siteID,
		PrimaryDomain:    primaryDomain,
		ServerNames:      serverNames,
		HTTPPort:         req.Bindings[0].Port,
		Bindings:         req.Bindings,
		RootPath:         req.RootPath,
		IndexFiles:       req.IndexFiles,
		AccessLogEnabled: req.AccessLogEnabled,
		AccessLogPath:    site.AccessLogPath,
		ErrorLogPath:     site.ErrorLogPath,
		RewritePath:      site.RewritePath,
		AccessLimitPath:  site.AccessLimitPath,
		HotlinkPath:      site.HotlinkPath,
		Document: nginx.DocumentData{
			AutoindexEnabled:   site.AutoindexEnabled,
			AutoindexExactSize: site.AutoindexExactSize,
			AutoindexLocaltime: site.AutoindexLocaltime,
			AutoindexFormat:    site.AutoindexFormat,
			ErrorPage404:       site.ErrorPage404,
			ErrorPage403:       site.ErrorPage403,
		},
	}
	configContent, err := nginx.Render(renderData)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "渲染配置失败: "+err.Error(), nil)
	}

	opID := app.NewOperationID()

	op := &repo.Operation{
		ID:         opID,
		Action:     "site.create",
		TargetType: "site",
		TargetID:   siteID,
		Status:     "pending",
		RequestID:  requestID,
		Actor:      "admin",
		Message:    fmt.Sprintf("创建网站 %s", primaryDomain),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := svc.opRepo.Create(op); err != nil {
		slog.Error("创建操作记录失败", "error", err)
	}

	changes := []agentclient.FileChangeRequest{
		{
			Type: "mkdir",
			Path: svc.cfg.Nginx.LogDir,
			Perm: 0755,
		},
		{
			Type:          "write",
			Path:          site.ConfigPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(configContent)),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          site.RewritePath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("")),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          site.HotlinkPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("# generated by nxpanel\n")),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          site.AccessLimitPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("")),
			Perm:          0644,
		},
	}

	if req.EnableAfterCreate {
		changes = append(changes, agentclient.FileChangeRequest{
			Type:   "symlink",
			Path:   site.EnabledPath,
			Target: "../sites-available/" + primaryDomain + ".conf",
		})
	}

	if req.CreateRoot {
		changes = append(changes, agentclient.FileChangeRequest{
			Type: "mkdir",
			Path: req.RootPath,
			Perm: 0755,
		})
		// 设置网站目录所属用户为 web_user（agent 端从配置解析）
		changes = append(changes, agentclient.FileChangeRequest{
			Type: "chown",
			Path: req.RootPath,
		})
		if req.CreateIndex {
			indexContent := svc.renderDefaultPage("new_site", primaryDomain)
			indexPath := filepath.Join(req.RootPath, "index.html")
			changes = append(changes, agentclient.FileChangeRequest{
				Type:          "write",
				Path:          indexPath,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(indexContent)),
				Perm:          0644,
			})
			changes = append(changes, agentclient.FileChangeRequest{
				Type: "chown",
				Path: indexPath,
			})

			if page404 := svc.renderDefaultPage("404", primaryDomain); page404 != "" {
				p404Path := filepath.Join(req.RootPath, "404.html")
				changes = append(changes, agentclient.FileChangeRequest{
					Type:          "write",
					Path:          p404Path,
					ContentBase64: base64.StdEncoding.EncodeToString([]byte(page404)),
					Perm:          0644,
				})
				changes = append(changes, agentclient.FileChangeRequest{
					Type: "chown",
					Path: p404Path,
				})
			}
		}
	}

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: req.EnableAfterCreate,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "文件事务失败: "+agentErr.Error(), nil)
	}

	if err := svc.siteRepo.Create(site); err != nil {
		slog.Error("创建站点数据库记录失败", "error", err)
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrInternalError, err.Error(), "")
		return nil, "", app.NewAppError(app.ErrInternalError, "创建站点失败: "+err.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")

	return site, opID, nil
}

func (svc *Service) UpdateDocument(ctx context.Context, siteID string, req *UpdateSiteDocumentRequest, requestID string) (*repo.Site, string, error) {
	indexFiles, err := ValidateDocument(req)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	currentContent, currentHash, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "读取配置文件失败: "+err.Error(), nil)
	}
	if req.ExpectedFileHash != "" && currentHash != req.ExpectedFileHash {
		return nil, "", app.NewAppError(app.ErrConfigDrifted, "配置文件已被外部修改，请刷新后重试", nil)
	}
	status := nginx.ValidateRequiredMarkers(currentContent, []string{nginx.MarkerNameRoot, nginx.MarkerNameDocument})
	if !status.Valid {
		return nil, "", app.NewAppError(app.ErrValidationFailed,
			fmt.Sprintf("配置文件缺少文档增强标识块: missing=%v duplicated=%v", status.Missing, status.Duplicated), nil)
	}

	site.IndexFiles = indexFiles
	site.AutoindexEnabled = req.AutoindexEnabled
	site.AutoindexExactSize = req.AutoindexExactSize
	site.AutoindexLocaltime = req.AutoindexLocaltime
	site.AutoindexFormat = req.AutoindexFormat
	site.ErrorPage404 = strings.TrimSpace(req.ErrorPage404)
	site.ErrorPage403 = strings.TrimSpace(req.ErrorPage403)

	patched, err := nginx.ApplyMarkerPatches(currentContent, []nginx.BlockPatch{
		{Name: nginx.MarkerNameRoot, Body: []byte(nginx.BuildRootBlock(site.RootPath, site.IndexFiles))},
		{Name: nginx.MarkerNameDocument, Body: []byte(nginx.BuildDocumentBlock(nginx.DocumentData{
			AutoindexEnabled:   site.AutoindexEnabled,
			AutoindexExactSize: site.AutoindexExactSize,
			AutoindexLocaltime: site.AutoindexLocaltime,
			AutoindexFormat:    site.AutoindexFormat,
			ErrorPage404:       site.ErrorPage404,
			ErrorPage403:       site.ErrorPage403,
		}))},
	})
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "标识块替换失败: "+err.Error(), nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.document.update", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("更新网站 %s 文档增强配置", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{{
			Type:          "write",
			Path:          site.ConfigPath,
			ContentBase64: base64.StdEncoding.EncodeToString(patched),
			Perm:          0644,
		}},
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "文件事务失败: "+agentErr.Error(), nil)
	}

	_ = svc.siteRepo.Update(site)
	_ = svc.opRepo.UpdateStatus(opID, "success")
	return site, opID, nil
}

func (svc *Service) List(page, pageSize int, keyword, status string) ([]*repo.Site, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return svc.siteRepo.List(page, pageSize, keyword, status)
}

func (svc *Service) Get(siteID string) (*repo.Site, *repo.SiteProxy, *repo.SiteSSL, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, nil, nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, nil, nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	proxy, _ := svc.proxyRepo.GetBySiteID(siteID)
	ssl, _ := svc.sslRepo.GetBySiteID(siteID)

	return site, proxy, ssl, nil
}

func (svc *Service) ImportWarnings(ctx context.Context, site *repo.Site) []string {
	if site == nil || site.ConfigPath != site.EnabledPath {
		return nil
	}
	return importWarningsForPaths(svc.agentRoots(ctx), site.ConfigPath, site.RootPath, site.AccessLogPath, site.ErrorLogPath)
}

// RefreshLogPaths reads the config file and re-extracts server-level
// access_log/error_log paths. If they differ from the DB values, the DB
// is updated. Returns the (possibly updated) site and whether a change
// was persisted. This keeps imported sites in sync when users edit
// config files externally.
func (svc *Service) RefreshLogPaths(ctx context.Context, site *repo.Site) (*repo.Site, bool) {
	if site == nil || site.ConfigPath == "" {
		return site, false
	}
	content, _, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return site, false
	}
	accessLog, errorLog := parser.ExtractServerLogPaths(string(content))
	changed := false
	if accessLog != site.AccessLogPath {
		site.AccessLogPath = accessLog
		changed = true
	}
	if errorLog != site.ErrorLogPath {
		site.ErrorLogPath = errorLog
		changed = true
	}
	if changed {
		_ = svc.siteRepo.Update(site)
		slog.Info("导入站点日志路径已同步", "site_id", site.ID, "access_log", accessLog, "error_log", errorLog)
	}
	return site, changed
}

// agentRoots 返回用于校验导入站点路径的白名单根目录。
// 优先使用 Agent 的实时白名单（FilesRoots），确保与 Agent 实际授权一致；
// Agent 不可达时降级回 API 本地配置快照，保持保守的告警行为。
func (svc *Service) agentRoots(ctx context.Context) []string {
	if svc.rootsFn != nil {
		if roots, err := svc.rootsFn(ctx); err == nil && len(roots) > 0 {
			return roots
		}
	}
	return app.BuildAllowedPathRoots(svc.cfg)
}

func (svc *Service) Update(ctx context.Context, siteID string, req *UpdateSiteRequest, requestID string) (*repo.Site, string, error) {
	if err := ValidateUpdate(req); err != nil {
		return nil, "", app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.update", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("修改网站 %s 基础配置", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	currentContent, currentHash, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "读取配置文件失败: "+err.Error(), nil)
	}

	if req.ExpectedFileHash != "" && currentHash != req.ExpectedFileHash {
		return nil, "", app.NewAppError(app.ErrConfigDrifted,
			"配置文件已被外部修改，请刷新后重试", nil)
	}

	requiredMarkers := []string{"SERVER-NAME", "LISTEN", "ROOT", "LOG"}
	status := nginx.ValidateRequiredMarkers(currentContent, requiredMarkers)
	if !status.Valid {
		return nil, "", app.NewAppError(app.ErrValidationFailed,
			fmt.Sprintf("配置文件缺少必要标识块: missing=%v duplicated=%v", status.Missing, status.Duplicated), nil)
	}

	var bindings []repo.Binding
	if len(req.Bindings) > 0 {
		for _, b := range req.Bindings {
			existing, _ := svc.siteRepo.GetByPrimaryDomain(b.Domain)
			if existing != nil && existing.ID != siteID {
				return nil, "", app.NewAppError(app.ErrConflict,
					fmt.Sprintf("域名 %s 已被站点 %s 使用", b.Domain, existing.ID), nil)
			}
		}
		bindings = req.Bindings
		site.PrimaryDomain = bindings[0].Domain
		site.HTTPPort = bindings[0].Port
	} else {
		json.Unmarshal([]byte(site.BindingsJSON), &bindings)
		if len(bindings) == 0 {
			var origDomains []string
			json.Unmarshal([]byte(site.DomainsJSON), &origDomains)
			for _, d := range origDomains {
				bindings = append(bindings, repo.Binding{Domain: d, Port: site.HTTPPort})
			}
		}
	}

	if req.HTTPSPort != 0 {
		site.HTTPSPort = req.HTTPSPort
	}
	if req.RootPath != "" {
		site.RootPath = req.RootPath
	}
	if req.IndexFiles != "" {
		site.IndexFiles = req.IndexFiles
	}
	site.AccessLogEnabled = req.AccessLogEnabled

	domains := make([]string, 0, len(bindings))
	for _, b := range bindings {
		domains = append(domains, b.Domain)
	}
	domainsJSON, _ := json.Marshal(domains)
	bindingsJSON, _ := json.Marshal(bindings)
	site.DomainsJSON = string(domainsJSON)
	site.BindingsJSON = string(bindingsJSON)

	serverNames := strings.Join(domains, " ")

	sslRec, _ := svc.sslRepo.GetBySiteID(siteID)
	var sslData *nginx.SSLData
	if sslRec != nil && sslRec.Enabled {
		sslData = &nginx.SSLData{
			Enabled:    true,
			ForceHTTPS: sslRec.ForceHTTPS,
		}
	}

	listenBodyStr := nginx.BuildListenBlock(&nginx.RenderData{
		HTTPPort:  site.HTTPPort,
		HTTPSPort: site.HTTPSPort,
		Bindings:  bindings,
		SSL:       sslData,
	})

	serverNameBody := nginx.BuildServerNameBlock(serverNames)

	rootBody := nginx.BuildRootBlock(site.RootPath, site.IndexFiles)

	logBodyStr := nginx.BuildLogBlock(&nginx.RenderData{
		AccessLogEnabled: site.AccessLogEnabled,
		AccessLogPath:    site.AccessLogPath,
		ErrorLogPath:     site.ErrorLogPath,
	})

	patched, err := nginx.ApplyMarkerPatches(currentContent, []nginx.BlockPatch{
		{Name: "SERVER-NAME", Body: []byte(serverNameBody)},
		{Name: "LISTEN", Body: []byte(listenBodyStr)},
		{Name: "ROOT", Body: []byte(rootBody)},
		{Name: "LOG", Body: []byte(logBodyStr)},
	})
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "标识块替换失败: "+err.Error(), nil)
	}

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{
			{
				Type:          "write",
				Path:          site.ConfigPath,
				ContentBase64: base64.StdEncoding.EncodeToString(patched),
				Perm:          0644,
			},
		},
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "文件事务失败: "+agentErr.Error(), nil)
	}

	_ = svc.siteRepo.Update(site)
	_ = svc.opRepo.UpdateStatus(opID, "success")

	return site, opID, nil
}

func (svc *Service) Enable(ctx context.Context, siteID string, req *EnableSiteRequest, requestID string) (string, string, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return "", "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return "", "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	if site.Status == "enabled" {
		return "", "", app.NewAppError(app.ErrConflict, "站点已经处于启用状态", nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.enable", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("启用网站 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{
			{
				Type:   "symlink",
				Path:   site.EnabledPath,
				Target: "../sites-available/" + site.PrimaryDomain + ".conf",
			},
		},
		TestNginx:   true,
		ReloadNginx: true,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return "", "", app.NewAppError(app.ErrAgentUnavailable, agentErr.Error(), nil)
	}

	_ = svc.siteRepo.UpdateStatus(siteID, "enabled")
	_ = svc.opRepo.UpdateStatus(opID, "success")

	return "enabled", opID, nil
}

func (svc *Service) Disable(ctx context.Context, siteID string, req *DisableSiteRequest, requestID string) (string, string, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return "", "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return "", "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	if site.Status == "disabled" {
		return "", "", app.NewAppError(app.ErrConflict, "站点已经处于禁用状态", nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.disable", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("禁用网站 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes: []agentclient.FileChangeRequest{
			{Type: "remove", Path: site.EnabledPath},
		},
		TestNginx:   true,
		ReloadNginx: true,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return "", "", app.NewAppError(app.ErrAgentUnavailable, agentErr.Error(), nil)
	}

	_ = svc.siteRepo.UpdateStatus(siteID, "disabled")
	_ = svc.opRepo.UpdateStatus(opID, "success")

	return "disabled", opID, nil
}

func (svc *Service) Delete(ctx context.Context, siteID string, req *DeleteSiteRequest, requestID string) (bool, string, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return false, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return false, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.delete", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("删除网站 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	changes := []agentclient.FileChangeRequest{
		{Type: "remove", Path: site.EnabledPath},
		{Type: "remove", Path: site.ConfigPath},
		{Type: "remove", Path: site.RewritePath},
		{Type: "remove", Path: site.AccessLimitPath},
	}

	if req.DeleteSSLFiles {
		ssl, sslErr := svc.sslRepo.GetBySiteID(siteID)
		if sslErr != nil {
			return false, "", app.NewAppError(app.ErrInternalError, "查询 SSL 配置失败: "+sslErr.Error(), nil)
		}
		if ssl != nil && ssl.CertPath != "" {
			sharedCount, sharedErr := svc.sslRepo.CountOtherSitesByCertPath(siteID, ssl.CertPath)
			if sharedErr != nil {
				return false, "", app.NewAppError(app.ErrInternalError, "检查证书共享状态失败: "+sharedErr.Error(), nil)
			}
			if sharedCount > 0 {
				return false, "", app.NewAppError(app.ErrConflict,
					fmt.Sprintf("该 SSL 证书仍被 %d 个其他站点使用，请先禁用相关站点的 SSL 或取消勾选删除证书文件", sharedCount), nil)
			}
			changes = append(changes, agentclient.FileChangeRequest{Type: "remove", Path: ssl.CertPath})
			if strings.TrimSpace(ssl.KeyPath) != "" {
				changes = append(changes, agentclient.FileChangeRequest{Type: "remove", Path: ssl.KeyPath})
			}
		}
	}

	if req.DeleteLogs {
		if strings.TrimSpace(site.AccessLogPath) != "" {
			changes = append(changes, agentclient.FileChangeRequest{Type: "remove", Path: site.AccessLogPath})
		}
		if strings.TrimSpace(site.ErrorLogPath) != "" {
			changes = append(changes, agentclient.FileChangeRequest{Type: "remove", Path: site.ErrorLogPath})
		}
	}

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: true,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return false, "", app.NewAppError(app.ErrAgentUnavailable, agentErr.Error(), nil)
	}
	if req.DeleteRoot && strings.TrimSpace(site.RootPath) != "" {
		if err := svc.agent.FilesRemove(ctx, []string{site.RootPath}); err != nil {
			_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, "删除网站根目录失败: "+err.Error(), "")
			return false, "", app.NewAppError(app.ErrAgentUnavailable, "删除网站根目录失败: "+err.Error(), nil)
		}
	}

	_ = svc.siteRepo.Delete(siteID)
	_ = svc.opRepo.UpdateStatus(opID, "success")

	return true, opID, nil
}

func (svc *Service) renderDefaultPage(pageType, domain string) string {
	if svc.settingsSvc != nil {
		tmpl := svc.settingsSvc.GetDefaultPageTemplate(pageType)
		if tmpl != "" {
			result := strings.ReplaceAll(tmpl, "{{domain}}", domain)
			result = strings.ReplaceAll(result, "{{year}}", fmt.Sprintf("%d", time.Now().UTC().Year()))
			return result
		}
	}
	return fmt.Sprintf("<html><body><h1>%s</h1></body></html>", domain)
}

func (svc *Service) ImportScan(ctx context.Context, requestID string) (*ImportScanResponse, error) {
	_ = requestID
	dumpResp, err := svc.agent.NginxDump(ctx, &agentclient.NginxDumpRequest{})
	if err != nil {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "nginx -T 执行失败: "+err.Error(), nil)
	}

	parsed := parser.ParseNginxDump(dumpResp.Stdout)

	managedPaths := svc.getManagedConfigPaths()

	// 取一次 Agent 实时白名单，循环内复用，避免每个站点一次 socket 调用。
	roots := svc.agentRoots(ctx)

	var items []ImportScanItem
	for _, p := range parsed {
		if p.SourceFile == "" {
			continue
		}
		if managedPaths[p.SourceFile] {
			continue
		}
		if len(p.ServerNames) == 0 {
			continue
		}
		warnings := importWarningsForPaths(roots, p.SourceFile, p.RootPath, p.AccessLogPath, p.ErrorLogPath)
		items = append(items, ImportScanItem{
			SourceFile:      p.SourceFile,
			ServerNames:     p.ServerNames,
			Listen:          p.Listen,
			RootPath:        p.RootPath,
			AccessLogPath:   p.AccessLogPath,
			ErrorLogPath:    p.ErrorLogPath,
			ConfigPathOK:    !containsImportWarningForPath(warnings, "配置文件"),
			RootPathOK:      !containsImportWarningForPath(warnings, "根目录"),
			AccessLogPathOK: !containsImportWarningForPath(warnings, "访问日志"),
			ErrorLogPathOK:  !containsImportWarningForPath(warnings, "错误日志"),
			Warnings:        warnings,
		})
	}

	return &ImportScanResponse{Items: items}, nil
}

func importWarningsForPaths(roots []string, configPath, rootPath, accessLogPath, errorLogPath string) []string {
	warnings := make([]string, 0, 4)
	if !isPathInRoots(configPath, roots) {
		warnings = append(warnings, "配置文件未在 Agent 白名单内，导入后“站点配置”不可用；请在 agent.allowed_roots 中加入该配置目录并重启 nxpanel-agent")
	}
	if !isPathInRoots(rootPath, roots) {
		warnings = append(warnings, "根目录未在 Agent 白名单内，导入后“文件管理”不可用；请在 nginx.allowed_root_prefixes 或 agent.allowed_roots 中加入该目录并重启 nxpanel-agent")
	}
	if strings.TrimSpace(accessLogPath) != "" && !isPathInRoots(accessLogPath, roots) {
		warnings = append(warnings, "访问日志未在 Agent 白名单内，导入后“访问日志”不可用；请在 nginx.allowed_log_prefixes 或 agent.allowed_roots 中加入该目录并重启 nxpanel-agent")
	}
	if strings.TrimSpace(errorLogPath) != "" && !isPathInRoots(errorLogPath, roots) {
		warnings = append(warnings, "错误日志未在 Agent 白名单内，导入后“错误日志”不可用；请在 nginx.allowed_log_prefixes 或 agent.allowed_roots 中加入该目录并重启 nxpanel-agent")
	}
	return warnings
}

func isPathInRoots(path string, roots []string) bool {
	if strings.TrimSpace(path) == "" {
		return true
	}
	cleanPath := filepath.Clean(path)
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		cleanRoot := filepath.Clean(root)
		rel, err := filepath.Rel(cleanRoot, cleanPath)
		if err != nil {
			continue
		}
		if rel != ".." && !strings.HasPrefix(rel, "../") {
			return true
		}
	}
	return false
}

func containsImportWarningForPath(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

func (svc *Service) getManagedConfigPaths() map[string]bool {
	paths := make(map[string]bool)
	sites, _, err := svc.siteRepo.List(1, 1000, "", "")
	if err != nil {
		return paths
	}
	for _, s := range sites {
		paths[s.ConfigPath] = true
		paths[s.EnabledPath] = true
	}
	return paths
}

func (svc *Service) Import(ctx context.Context, req *ImportSiteRequest, requestID string) (*ImportSiteResponse, error) {
	if req.SourceFile == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "source_file 不能为空", nil)
	}

	dumpResp, err := svc.agent.NginxDump(ctx, &agentclient.NginxDumpRequest{})
	if err != nil {
		return nil, app.NewAppError(app.ErrAgentUnavailable, "nginx -T 执行失败: "+err.Error(), nil)
	}

	parsed := parser.ParseNginxDump(dumpResp.Stdout)

	var target *parser.ParsedServer
	for _, p := range parsed {
		if p.SourceFile == req.SourceFile {
			target = p
			break
		}
	}
	if target == nil {
		return nil, app.NewAppError(app.ErrNotFound, "未找到配置文件对应的 server block: "+req.SourceFile, nil)
	}

	if len(target.ServerNames) == 0 {
		return nil, app.NewAppError(app.ErrValidationFailed, "该 server block 没有 server_name，无法导入", nil)
	}

	primaryDomain := target.ServerNames[0]
	existing, _ := svc.siteRepo.GetByPrimaryDomain(primaryDomain)
	if existing != nil {
		return nil, app.NewAppError(app.ErrConflict,
			fmt.Sprintf("域名 %s 已被站点 %s 使用", primaryDomain, existing.ID), nil)
	}

	siteID := app.NewSiteID()
	httpPort := 80
	if len(target.Listen) > 0 {
		if p, errParse := strconv.Atoi(strings.Fields(target.Listen[0])[0]); errParse == nil {
			httpPort = p
		}
	}

	domains := target.ServerNames
	domainsJSON, _ := json.Marshal(domains)

	site := &repo.Site{
		ID:               siteID,
		PrimaryDomain:    primaryDomain,
		DomainsJSON:      string(domainsJSON),
		BindingsJSON:     string(domainsJSON),
		Status:           "enabled",
		HTTPPort:         httpPort,
		HTTPSPort:        443,
		RootPath:         target.RootPath,
		IndexFiles:       "index.html index.htm",
		AccessLogEnabled: target.AccessLogPath != "",
		AccessLogPath:    target.AccessLogPath,
		ErrorLogPath:     target.ErrorLogPath,
		ConfigPath:       req.SourceFile,
		EnabledPath:      req.SourceFile,
		RewritePath:      filepath.Join(svc.cfg.Nginx.PanelDir, "rewrite", primaryDomain+".conf"),
		AccessLimitPath:  filepath.Join(svc.cfg.Nginx.PanelDir, "access-limit", primaryDomain+".conf"),
		HotlinkPath:      filepath.Join(svc.cfg.Nginx.PanelDir, "hotlink", primaryDomain+".conf"),
		MarkerVersion:    1,
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.import", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("导入旧站点 %s", primaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	rewriteContent := ""
	changes := []agentclient.FileChangeRequest{
		{
			Type:          "write",
			Path:          site.HotlinkPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("# generated by nxpanel\n")),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          site.RewritePath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(rewriteContent)),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          site.AccessLimitPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte("")),
			Perm:          0644,
		},
	}
	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   false,
		ReloadNginx: false,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "导入事务失败: "+agentErr.Error(), nil)
	}

	if err := svc.siteRepo.Create(site); err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrInternalError, err.Error(), "")
		return nil, app.NewAppError(app.ErrInternalError, "创建站点记录失败: "+err.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")

	return &ImportSiteResponse{
		SiteID:      siteID,
		OperationID: opID,
		Status:      site.Status,
	}, nil
}
