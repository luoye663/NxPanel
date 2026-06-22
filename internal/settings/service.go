package settings

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
)

type SecurityConfigReloader interface {
	ReloadSecurityConfig(cfg *app.Config)
}

type Service struct {
	settingsRepo   *repo.SettingsRepo
	siteRepo       *repo.SiteRepo
	certRepo       *repo.CertificateRepo
	sslRepo        *repo.SSLRepo
	proxyRepo      *repo.ProxyRepo
	opRepo         *repo.OperationRepo
	agent          *agentclient.Client
	cfg            *app.Config
	configReloader SecurityConfigReloader
	taskSvc        ScheduledTaskService
}

func NewService(
	settingsRepo *repo.SettingsRepo,
	siteRepo *repo.SiteRepo,
	certRepo *repo.CertificateRepo,
	sslRepo *repo.SSLRepo,
	proxyRepo *repo.ProxyRepo,
	opRepo *repo.OperationRepo,
	agent *agentclient.Client,
	cfg *app.Config,
) *Service {
	return &Service{
		settingsRepo: settingsRepo,
		siteRepo:     siteRepo,
		certRepo:     certRepo,
		sslRepo:      sslRepo,
		proxyRepo:    proxyRepo,
		opRepo:       opRepo,
		agent:        agent,
		cfg:          cfg,
	}
}

func (svc *Service) SetConfigReloader(reloader SecurityConfigReloader) {
	svc.configReloader = reloader
}

func (svc *Service) AttachScheduledTasks(taskSvc ScheduledTaskService) error {
	svc.taskSvc = taskSvc
	if taskSvc == nil {
		return nil
	}
	return taskSvc.Register(NewNginxLogRotationTaskHandler(svc))
}

// ============================================================
// 默认页面模板
// KV 缓存 + Agent 文件系统双写
// 文件路径：{panel_dir}/default-pages/{new_site,404,site_not_found,site_disabled}.html
// ============================================================

var compiledInDefaultPages = map[string]string{
	"new_site": `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{domain}}</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: #f0f2f5; color: #333; }
        .container { text-align: center; padding: 60px 40px; background: #fff; border-radius: 8px; box-shadow: 0 2px 12px rgba(0,0,0,0.08); max-width: 560px; width: 90%; }
        h1 { font-size: 28px; margin-bottom: 12px; color: #1a1a1a; }
        p { font-size: 16px; color: #666; line-height: 1.6; }
        .footer { margin-top: 32px; font-size: 13px; color: #999; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Welcome to {{domain}}</h1>
        <p>网站已成功创建，请上传网站文件替换此页面。</p>
        <div class="footer">&copy; {{year}} {{domain}}</div>
    </div>
</body>
</html>`,
	"404": `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>404 Not Found</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: #f0f2f5; color: #333; }
        .container { text-align: center; padding: 60px 40px; background: #fff; border-radius: 8px; box-shadow: 0 2px 12px rgba(0,0,0,0.08); max-width: 560px; width: 90%; }
        .code { font-size: 72px; font-weight: 700; color: #e6a23c; margin-bottom: 16px; }
        h1 { font-size: 24px; margin-bottom: 12px; color: #1a1a1a; }
        p { font-size: 16px; color: #666; line-height: 1.6; }
        a { color: #409eff; text-decoration: none; }
        a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="container">
        <div class="code">404</div>
        <h1>页面未找到</h1>
        <p>您请求的页面不存在或已被移除。</p>
        <p style="margin-top: 20px"><a href="/">返回首页</a></p>
    </div>
</body>
</html>`,
	"site_not_found": `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Site Not Found</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: #f0f2f5; color: #333; }
        .container { text-align: center; padding: 60px 40px; background: #fff; border-radius: 8px; box-shadow: 0 2px 12px rgba(0,0,0,0.08); max-width: 560px; width: 90%; }
        .icon { font-size: 64px; margin-bottom: 16px; }
        h1 { font-size: 24px; margin-bottom: 12px; color: #1a1a1a; }
        p { font-size: 16px; color: #666; line-height: 1.6; }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">&#128274;</div>
        <h1>网站不存在</h1>
        <p>您访问的网站尚未在服务器上配置，请检查域名是否正确。</p>
    </div>
</body>
</html>`,
	"site_disabled": `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Site Disabled</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; display: flex; align-items: center; justify-content: center; min-height: 100vh; background: #f0f2f5; color: #333; }
        .container { text-align: center; padding: 60px 40px; background: #fff; border-radius: 8px; box-shadow: 0 2px 12px rgba(0,0,0,0.08); max-width: 560px; width: 90%; }
        .icon { font-size: 64px; margin-bottom: 16px; }
        h1 { font-size: 24px; margin-bottom: 12px; color: #1a1a1a; }
        p { font-size: 16px; color: #666; line-height: 1.6; }
    </style>
</head>
<body>
    <div class="container">
        <div class="icon">&#9888;&#65039;</div>
        <h1>网站已停用</h1>
        <p>该网站已被管理员停用，暂时无法访问。如有疑问请联系网站管理员。</p>
    </div>
</body>
</html>`,
}

var pageFileNames = map[string]string{
	"new_site":       "new_site.html",
	"404":            "404.html",
	"site_not_found": "site_not_found.html",
	"site_disabled":  "site_disabled.html",
}

const defaultPagesKVKey = "default_pages"

func (svc *Service) defaultPagesDir() string {
	return filepath.Join(svc.cfg.Nginx.PanelDir, "default-pages")
}

func (svc *Service) pageFilePath(pageType string) string {
	name, ok := pageFileNames[pageType]
	if !ok {
		return ""
	}
	return filepath.Join(svc.defaultPagesDir(), name)
}

func (svc *Service) GetDefaultPages() (*DefaultPagesSettings, error) {
	val, err := svc.settingsRepo.Get(defaultPagesKVKey)
	if err != nil {
		return nil, err
	}
	if val != "" {
		var pages DefaultPagesSettings
		if err := json.Unmarshal([]byte(val), &pages); err != nil {
			slog.Warn("解析 default_pages 设置失败，使用默认值", "error", err)
		} else {
			return &pages, nil
		}
	}
	defaults := svc.compiledDefaults()
	if svc.agent != nil {
		svc.deployDefaultsIfEmpty(context.Background(), defaults)
	}
	return defaults, nil
}

func (svc *Service) compiledDefaults() *DefaultPagesSettings {
	return &DefaultPagesSettings{
		NewSitePage:      compiledInDefaultPages["new_site"],
		Page404:          compiledInDefaultPages["404"],
		SiteNotFoundPage: compiledInDefaultPages["site_not_found"],
		SiteDisabledPage: compiledInDefaultPages["site_disabled"],
	}
}

func (svc *Service) deployDefaultsIfEmpty(ctx context.Context, defaults *DefaultPagesSettings) {
	dir := svc.defaultPagesDir()
	var changes []agentclient.FileChangeRequest
	changes = append(changes, agentclient.FileChangeRequest{Type: "mkdir", Path: dir, Perm: 0755})

	pageMap := map[string]string{
		"new_site":       defaults.NewSitePage,
		"404":            defaults.Page404,
		"site_not_found": defaults.SiteNotFoundPage,
		"site_disabled":  defaults.SiteDisabledPage,
	}
	for pType, content := range pageMap {
		fp := svc.pageFilePath(pType)
		if fp == "" {
			continue
		}
		changes = append(changes, agentclient.FileChangeRequest{
			Type:          "write",
			Path:          fp,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)),
			Perm:          0644,
		})
	}

	data, _ := json.Marshal(defaults)
	_ = svc.settingsRepo.Set(defaultPagesKVKey, string(data))

	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := svc.agent.ApplyTransaction(ctx2, &agentclient.TransactionRequest{
		OperationID: "deploy-default-pages",
		Changes:     changes,
		TestNginx:   false,
		ReloadNginx: false,
	}); err != nil {
		slog.Warn("部署默认页面模板文件失败", "error", err)
	}
}

func (svc *Service) UpdateDefaultPages(ctx context.Context, req *UpdateDefaultPagesRequest, requestID string) error {
	pages := &DefaultPagesSettings{
		NewSitePage:      req.NewSitePage,
		Page404:          req.Page404,
		SiteNotFoundPage: req.SiteNotFoundPage,
		SiteDisabledPage: req.SiteDisabledPage,
	}

	data, _ := json.Marshal(pages)
	if err := svc.settingsRepo.Set(defaultPagesKVKey, string(data)); err != nil {
		return app.NewAppError(app.ErrInternalError, "保存默认页面设置失败: "+err.Error(), nil)
	}

	if svc.agent != nil {
		dir := svc.defaultPagesDir()
		var changes []agentclient.FileChangeRequest
		changes = append(changes, agentclient.FileChangeRequest{Type: "mkdir", Path: dir, Perm: 0755})

		pageMap := map[string]string{
			"new_site":       pages.NewSitePage,
			"404":            pages.Page404,
			"site_not_found": pages.SiteNotFoundPage,
			"site_disabled":  pages.SiteDisabledPage,
		}
		for pType, content := range pageMap {
			fp := svc.pageFilePath(pType)
			if fp == "" {
				continue
			}
			changes = append(changes, agentclient.FileChangeRequest{
				Type:          "write",
				Path:          fp,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)),
				Perm:          0644,
			})
		}

		if _, err := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
			OperationID: app.NewOperationID(),
			Changes:     changes,
			TestNginx:   false,
			ReloadNginx: false,
		}); err != nil {
			slog.Warn("回写默认页面模板文件失败", "error", err)
		}
	}

	return nil
}

// ============================================================
// 默认站点
// ============================================================

func (svc *Service) GetDefaultSite() (*DefaultSiteSettings, error) {
	val, err := svc.settingsRepo.Get("default_site")
	if err != nil {
		return nil, err
	}
	if val == "" {
		return &DefaultSiteSettings{}, nil
	}
	var ds DefaultSiteSettings
	if err := json.Unmarshal([]byte(val), &ds); err != nil {
		return &DefaultSiteSettings{}, nil
	}
	return &ds, nil
}

func (svc *Service) UpdateDefaultSite(ctx context.Context, req *UpdateDefaultSiteRequest, requestID string) (*DefaultSiteSettings, error) {
	oldDS, _ := svc.GetDefaultSite()
	oldSiteID := oldDS.SiteID

	newSiteID := req.SiteID

	if newSiteID != "" {
		site, err := svc.siteRepo.GetByID(newSiteID)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if site == nil {
			return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
		}
	}

	var changes []agentclient.FileChangeRequest

	if oldSiteID != "" && oldSiteID != newSiteID {
		patched, err := svc.reRenderSiteWithoutDefaultServer(ctx, oldSiteID)
		if err != nil {
			slog.Warn("移除旧默认站点 default_server 失败", "site_id", oldSiteID, "error", err)
		} else if patched != nil {
			changes = append(changes, *patched)
		}
	}

	if newSiteID != "" && newSiteID != oldSiteID {
		patched, err := svc.reRenderSiteWithDefaultServer(ctx, newSiteID)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, "为新默认站点添加 default_server 失败: "+err.Error(), nil)
		}
		if patched != nil {
			changes = append(changes, *patched)
		}
	}

	if newSiteID == "" && oldSiteID != "" {
		patched, err := svc.reRenderSiteWithoutDefaultServer(ctx, oldSiteID)
		if err != nil {
			slog.Warn("移除旧默认站点 default_server 失败", "site_id", oldSiteID, "error", err)
		} else if patched != nil {
			changes = append(changes, *patched)
		}
	}

	if len(changes) > 0 {
		opID := app.NewOperationID()
		_ = svc.opRepo.Create(&repo.Operation{
			ID: opID, Action: "settings.update_default_site", TargetType: "settings", TargetID: "default_site",
			Status: "pending", RequestID: requestID, Actor: "admin",
			Message:   "更新默认站点设置",
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
		})
		if _, err := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
			OperationID: opID,
			Changes:     changes,
			TestNginx:   true,
			ReloadNginx: true,
		}); err != nil {
			_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
			return nil, app.NewAppError(app.ErrAgentUnavailable, "应用默认站点配置失败: "+err.Error(), nil)
		}
		_ = svc.opRepo.UpdateStatus(opID, "success")
	}

	result := &DefaultSiteSettings{}
	if newSiteID != "" {
		site, _ := svc.siteRepo.GetByID(newSiteID)
		if site != nil {
			result = &DefaultSiteSettings{SiteID: newSiteID, PrimaryDomain: site.PrimaryDomain}
		}
	}
	data, _ := json.Marshal(result)
	if err := svc.settingsRepo.Set("default_site", string(data)); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, "保存默认站点设置失败: "+err.Error(), nil)
	}

	return result, nil
}

func (svc *Service) reRenderSiteWithDefaultServer(ctx context.Context, siteID string) (*agentclient.FileChangeRequest, error) {
	return svc.patchSiteListenBlock(ctx, siteID, true)
}

func (svc *Service) reRenderSiteWithoutDefaultServer(ctx context.Context, siteID string) (*agentclient.FileChangeRequest, error) {
	return svc.patchSiteListenBlock(ctx, siteID, false)
}

func (svc *Service) patchSiteListenBlock(ctx context.Context, siteID string, isDefault bool) (*agentclient.FileChangeRequest, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil || site == nil {
		return nil, fmt.Errorf("站点不存在: %s", siteID)
	}

	currentContent, _, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var bindings []repo.Binding
	if site.BindingsJSON != "" {
		json.Unmarshal([]byte(site.BindingsJSON), &bindings)
	}

	sslRec, _ := svc.sslRepo.GetBySiteID(siteID)
	var sslData *nginx.SSLData
	if sslRec != nil && sslRec.Enabled {
		sslData = &nginx.SSLData{
			Enabled:    true,
			Mode:       sslRec.Mode,
			CertPath:   sslRec.CertPath,
			KeyPath:    sslRec.KeyPath,
			ForceHTTPS: sslRec.ForceHTTPS,
		}
	}

	renderData := &nginx.RenderData{
		HTTPPort:         site.HTTPPort,
		HTTPSPort:        site.HTTPSPort,
		Bindings:         bindings,
		SSL:              sslData,
		DefaultServer:    isDefault,
		AccessLogEnabled: site.AccessLogEnabled,
		AccessLogPath:    site.AccessLogPath,
		ErrorLogPath:     site.ErrorLogPath,
	}

	listenBody := nginx.BuildListenBlock(renderData)

	patched, err := nginx.ApplyMarkerPatches(currentContent, []nginx.BlockPatch{
		{Name: "LISTEN", Body: []byte(listenBody)},
	})
	if err != nil {
		return nil, fmt.Errorf("替换 LISTEN 标识块失败: %w", err)
	}

	return &agentclient.FileChangeRequest{
		Type:          "write",
		Path:          site.ConfigPath,
		ContentBase64: base64.StdEncoding.EncodeToString(patched),
		Perm:          0644,
	}, nil
}

// ============================================================
// HTTPS 防窜站
// ============================================================

func (svc *Service) GetHTTPSHijack() (*HTTPSHijackSettings, error) {
	val, err := svc.settingsRepo.Get("https_hijack")
	if err != nil {
		return nil, err
	}
	if val == "" {
		return &HTTPSHijackSettings{
			Enabled:      false,
			ReturnStatus: 444,
			CertMode:     "self_signed",
		}, nil
	}
	var h HTTPSHijackSettings
	if err := json.Unmarshal([]byte(val), &h); err != nil {
		return &HTTPSHijackSettings{
			Enabled:      false,
			ReturnStatus: 444,
			CertMode:     "self_signed",
		}, nil
	}
	return &h, nil
}

func (svc *Service) UpdateHTTPSHijack(ctx context.Context, req *UpdateHTTPSHijackRequest, requestID string) (*HTTPSHijackSettings, error) {
	if req.Enabled && req.ReturnStatus < 400 {
		return nil, app.NewAppError(app.ErrValidationFailed, "返回状态码必须在 400-599 之间", nil)
	}

	certMode := req.CertMode
	if certMode == "" {
		certMode = "self_signed"
	}

	settings := &HTTPSHijackSettings{
		Enabled:      req.Enabled,
		ReturnStatus: req.ReturnStatus,
		CertMode:     certMode,
	}

	panelDir := svc.cfg.Nginx.PanelDir
	confDir := filepath.Join(panelDir, "conf.d")
	httpsDefaultConf := filepath.Join(confDir, "00-https-default.conf")

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "settings.update_https_hijack", TargetType: "settings", TargetID: "https_hijack",
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   "更新 HTTPS 防窜站设置",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	var changes []agentclient.FileChangeRequest

	if req.Enabled {
		var certPath, keyPath string
		var err error

		if certMode == "custom" && req.CustomCertID != "" {
			cert, err := svc.certRepo.GetByID(req.CustomCertID)
			if err != nil || cert == nil {
				_ = svc.opRepo.UpdateError(opID, "failed", app.ErrNotFound, "证书不存在", "")
				return nil, app.NewAppError(app.ErrNotFound, "证书不存在", nil)
			}
			certPath = cert.CertPath
			keyPath = cert.KeyPath
			settings.CertPath = certPath
			settings.KeyPath = keyPath
			settings.CustomCertID = req.CustomCertID
		} else {
			certPath, keyPath, err = svc.ensureSelfSignedCert(ctx)
			if err != nil {
				_ = svc.opRepo.UpdateError(opID, "failed", app.ErrInternalError, "生成自签证书失败: "+err.Error(), "")
				return nil, app.NewAppError(app.ErrInternalError, "生成自签证书失败: "+err.Error(), nil)
			}
			settings.CertPath = certPath
			settings.KeyPath = keyPath
			settings.CertMode = "self_signed"
		}

		returnStatus := req.ReturnStatus
		if returnStatus == 0 {
			returnStatus = 444
		}
		settings.ReturnStatus = returnStatus

		conf := fmt.Sprintf(`server {
    listen 443 ssl default_server;
    server_name _;

    ssl_certificate %s;
    ssl_certificate_key %s;
    ssl_protocols TLSv1.2 TLSv1.3;

    return %d;
}
`, certPath, keyPath, returnStatus)

		changes = append(changes, agentclient.FileChangeRequest{
			Type:          "write",
			Path:          httpsDefaultConf,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(conf)),
			Perm:          0644,
		})
	} else {
		changes = append(changes, agentclient.FileChangeRequest{
			Type: "remove",
			Path: httpsDefaultConf,
		})
	}

	if _, err := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: true,
	}); err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		return nil, app.NewAppError(app.ErrAgentUnavailable, "应用 HTTPS 防窜站配置失败: "+err.Error(), nil)
	}

	data, _ := json.Marshal(settings)
	if err := svc.settingsRepo.Set("https_hijack", string(data)); err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrInternalError, err.Error(), "")
		return nil, app.NewAppError(app.ErrInternalError, "保存 HTTPS 防窜站设置失败: "+err.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")
	return settings, nil
}

func (svc *Service) ensureSelfSignedCert(ctx context.Context) (certPath, keyPath string, err error) {
	panelDir := svc.cfg.Nginx.PanelDir
	certDir := filepath.Join(panelDir, "ssl")
	certPath = filepath.Join(certDir, "https-hijack-default.crt")
	keyPath = filepath.Join(certDir, "https-hijack-default.key")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("生成 ECDSA 密钥失败: %w", err)
	}

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: "nxpanel-https-hijack"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"*"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return "", "", fmt.Errorf("创建自签证书失败: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyBytes, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	changes := []agentclient.FileChangeRequest{
		{Type: "mkdir", Path: certDir, Perm: 0700},
		{Type: "write", Path: certPath, ContentBase64: base64.StdEncoding.EncodeToString(certPEM), Perm: 0600},
		{Type: "write", Path: keyPath, ContentBase64: base64.StdEncoding.EncodeToString(keyPEM), Perm: 0600},
	}

	if _, err := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: "self-signed-cert",
		Changes:     changes,
		TestNginx:   false,
		ReloadNginx: false,
	}); err != nil {
		return "", "", fmt.Errorf("写入自签证书文件失败: %w", err)
	}

	return certPath, keyPath, nil
}

// GetDefaultPageTemplate 获取指定类型的默认页面模板
func (svc *Service) GetDefaultPageTemplate(pageType string) string {
	pages, err := svc.GetDefaultPages()
	if err != nil {
		return ""
	}
	switch pageType {
	case "new_site":
		return pages.NewSitePage
	case "404":
		return pages.Page404
	case "site_not_found":
		return pages.SiteNotFoundPage
	case "site_disabled":
		return pages.SiteDisabledPage
	default:
		return ""
	}
}

func (svc *Service) GetLogRotate(ctx context.Context) (*LogRotateSettings, error) {
	if svc.taskSvc != nil {
		task, err := svc.taskSvc.GetBySource(ctx, nginxLogRotationSourceType, nginxLogRotationSourceID)
		if err != nil {
			return nil, err
		}
		if task != nil {
			params, err := svc.decodeNginxLogRotationParams(task.ParamsJSON)
			if err != nil {
				return nil, err
			}
			return &LogRotateSettings{
				Enabled:  task.Enabled,
				Interval: task.ScheduleExpr,
				MaxCount: params.MaxCount,
				MaxAge:   params.MaxAge,
				MinSize:  params.MinSize,
			}, nil
		}
	}
	return &LogRotateSettings{
		Enabled:  defaultNginxLogRotationEnabled,
		Interval: defaultNginxLogRotationInterval,
		MaxCount: defaultNginxLogRotationMaxCount,
		MaxAge:   defaultNginxLogRotationMaxAge,
		MinSize:  defaultNginxLogRotationMinSize,
	}, nil
}

func (svc *Service) UpdateLogRotate(ctx context.Context, req *UpdateLogRotateRequest, requestID string) (*LogRotateSettings, error) {
	current, err := svc.GetLogRotate(ctx)
	if err != nil {
		return nil, err
	}
	changed := false

	if req.Interval != nil {
		if _, err := time.ParseDuration(*req.Interval); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, "interval 格式无效，例如: 1h, 24h, 168h", nil)
		}
		if _, err := scheduledtask.CompileSchedule(scheduledtask.ScheduleInterval, *req.Interval, "UTC"); err != nil {
			return nil, app.ErrValidationFailedMsg("interval 无效: "+err.Error(), nil)
		}
		current.Interval = *req.Interval
		changed = true
	}
	if req.MaxCount != nil {
		if *req.MaxCount < 1 || *req.MaxCount > 1000 {
			return nil, app.NewAppError(app.ErrValidationFailed, "max_count 必须在 1-1000 之间", nil)
		}
		current.MaxCount = *req.MaxCount
		changed = true
	}
	if req.MaxAge != nil {
		maxAge, err := time.ParseDuration(*req.MaxAge)
		if err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, "max_age 格式无效，例如: 720h", nil)
		}
		if maxAge < time.Hour || maxAge > 8760*time.Hour {
			return nil, app.NewAppError(app.ErrValidationFailed, "max_age 必须在 1h-8760h 之间", nil)
		}
		current.MaxAge = *req.MaxAge
		changed = true
	}
	if req.MinSize != nil {
		if _, err := parseLogRotationSize(*req.MinSize); err != nil {
			return nil, app.ErrValidationFailedMsg(err.Error(), nil)
		}
		current.MinSize = *req.MinSize
		changed = true
	}
	if req.Enabled != nil {
		current.Enabled = *req.Enabled
		changed = true
	}

	if changed {
		// 日志切割配置以 scheduled_tasks 为事实来源，不再回写 nginx.log_rotate_* 到 YAML。
		if err := svc.SyncNginxLogRotationSystemTask(ctx, *current); err != nil {
			return nil, err
		}
	}

	slog.Info("日志切割设置已更新", "enabled", current.Enabled, "interval", current.Interval, "min_size", current.MinSize)
	return svc.GetLogRotate(ctx)
}

// ============================================================
// 安全设置（API 登录限流 / 会话 / 可信代理 / CAPTCHA）

func (svc *Service) GetSecuritySettings() *SecuritySettings {
	masked := "********"
	if svc.cfg.API.Captcha.SecretKey == "" {
		masked = ""
	}
	proxies := svc.cfg.API.TrustedProxies
	if proxies == nil {
		proxies = []string{}
	}
	return &SecuritySettings{
		LoginPath:              svc.cfg.API.LoginPath,
		PublicHealth:           svc.cfg.API.PublicHealth,
		RateLimitMaxFailures:   svc.cfg.API.RateLimit.MaxFailures,
		RateLimitWindow:        svc.cfg.API.RateLimit.Window,
		MaxSessions:            svc.cfg.API.MaxSessions,
		BindSessionIP:          svc.cfg.API.BindSessionIP,
		BindSessionUA:          svc.cfg.API.BindSessionUA,
		TrustedProxies:         proxies,
		CaptchaProvider:        svc.cfg.API.Captcha.Provider,
		CaptchaSiteKey:         svc.cfg.API.Captcha.SiteKey,
		CaptchaSecretKeyMasked: masked,
		CaptchaTriggerAfter:    svc.cfg.API.Captcha.TriggerAfterFailures,
		TLSEnabled:             svc.cfg.API.TLS.Enabled,
		TLSCert:                svc.cfg.API.TLS.Cert,
		TLSKey:                 svc.cfg.API.TLS.Key,
		TLSCertValidity:        svc.cfg.API.TLS.CertValidity,
	}
}

func (svc *Service) UpdateSecuritySettings(ctx context.Context, req *UpdateSecuritySettingsRequest) (*SecuritySettings, error) {
	var fields []agentclient.ConfigWriteBackField

	if req.LoginPath != nil {
		if err := app.ValidateLoginPath(*req.LoginPath); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, "login_path 无效: "+err.Error(), nil)
		}
		svc.cfg.API.LoginPath = *req.LoginPath
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.login_path", Value: *req.LoginPath})
	}
	if req.PublicHealth != nil {
		svc.cfg.API.PublicHealth = *req.PublicHealth
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.public_health", Value: strconv.FormatBool(*req.PublicHealth)})
	}
	if req.RateLimitMaxFailures != nil {
		if *req.RateLimitMaxFailures < 1 {
			return nil, app.NewAppError(app.ErrValidationFailed, "max_failures 必须大于 0", nil)
		}
		svc.cfg.API.RateLimit.MaxFailures = *req.RateLimitMaxFailures
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.rate_limit.max_failures", Value: strconv.Itoa(*req.RateLimitMaxFailures)})
	}
	if req.RateLimitWindow != nil {
		if _, err := time.ParseDuration(*req.RateLimitWindow); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, "window 格式无效，例如: 15m, 1h", nil)
		}
		svc.cfg.API.RateLimit.Window = *req.RateLimitWindow
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.rate_limit.window", Value: *req.RateLimitWindow})
	}
	if req.MaxSessions != nil {
		if *req.MaxSessions < 1 {
			return nil, app.NewAppError(app.ErrValidationFailed, "max_sessions 必须大于 0", nil)
		}
		svc.cfg.API.MaxSessions = *req.MaxSessions
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.max_sessions", Value: strconv.Itoa(*req.MaxSessions)})
	}
	if req.BindSessionIP != nil {
		svc.cfg.API.BindSessionIP = *req.BindSessionIP
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.bind_session_ip", Value: strconv.FormatBool(*req.BindSessionIP)})
	}
	if req.BindSessionUA != nil {
		svc.cfg.API.BindSessionUA = *req.BindSessionUA
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.bind_session_ua", Value: strconv.FormatBool(*req.BindSessionUA)})
	}
	if req.TrustedProxies != nil {
		svc.cfg.API.TrustedProxies = *req.TrustedProxies
		val := ""
		if len(*req.TrustedProxies) > 0 {
			val = strings.Join(*req.TrustedProxies, ",")
		}
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.trusted_proxies", Value: val})
	}
	if req.CaptchaProvider != nil {
		switch *req.CaptchaProvider {
		case "none", "turnstile", "hcaptcha":
			svc.cfg.API.Captcha.Provider = *req.CaptchaProvider
			fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.captcha.provider", Value: *req.CaptchaProvider})
		default:
			return nil, app.NewAppError(app.ErrValidationFailed, "captcha provider 无效，可选: none / turnstile / hcaptcha", nil)
		}
	}
	if req.CaptchaSiteKey != nil {
		svc.cfg.API.Captcha.SiteKey = *req.CaptchaSiteKey
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.captcha.site_key", Value: *req.CaptchaSiteKey})
	}
	if req.CaptchaSecretKey != nil && *req.CaptchaSecretKey != "" {
		svc.cfg.API.Captcha.SecretKey = *req.CaptchaSecretKey
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.captcha.secret_key", Value: *req.CaptchaSecretKey})
	}
	if req.CaptchaTriggerAfter != nil {
		if *req.CaptchaTriggerAfter < 0 {
			return nil, app.NewAppError(app.ErrValidationFailed, "trigger_after_failures 不能为负数", nil)
		}
		svc.cfg.API.Captcha.TriggerAfterFailures = *req.CaptchaTriggerAfter
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.captcha.trigger_after_failures", Value: strconv.Itoa(*req.CaptchaTriggerAfter)})
	}
	if req.TLSEnabled != nil {
		svc.cfg.API.TLS.Enabled = *req.TLSEnabled
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.tls.enabled", Value: strconv.FormatBool(*req.TLSEnabled)})
	}
	if req.TLSCert != nil {
		svc.cfg.API.TLS.Cert = *req.TLSCert
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.tls.cert", Value: *req.TLSCert})
	}
	if req.TLSKey != nil {
		svc.cfg.API.TLS.Key = *req.TLSKey
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.tls.key", Value: *req.TLSKey})
	}
	if req.TLSCertValidity != nil {
		if _, err := time.ParseDuration(*req.TLSCertValidity); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, "cert_validity 格式无效，例如: 8760h", nil)
		}
		svc.cfg.API.TLS.CertValidity = *req.TLSCertValidity
		fields = append(fields, agentclient.ConfigWriteBackField{Key: "api.tls.cert_validity", Value: *req.TLSCertValidity})
	}
	if svc.agent != nil && len(fields) > 0 {
		if err := svc.agent.WriteBackConfig(ctx, &agentclient.ConfigWriteBackRequest{Fields: fields}); err != nil {
			slog.Warn("通过 Agent 回写安全配置失败", "error", err)
			return nil, app.NewAppError(app.ErrAgentUnavailable, "保存配置失败: "+err.Error(), nil)
		}
	}

	if svc.configReloader != nil {
		svc.configReloader.ReloadSecurityConfig(svc.cfg)
	}

	slog.Info("安全设置已更新")
	return svc.GetSecuritySettings(), nil
}
