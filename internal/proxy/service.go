// proxy 包 — 反向代理业务服务
//
// ProxyService 封装反向代理配置的读写逻辑：
//   - 获取站点的反代配置列表
//   - 创建/修改/删除反代配置
//   - 支持多代理、缓存配置
//   - 修改后重新渲染 Nginx 配置并通过 agent 写入文件
package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/security"
)

// Service 反向代理业务服务
type Service struct {
	siteRepo            *repo.SiteRepo
	proxyRepo           *repo.ProxyRepo
	upstreamRepo        *repo.UpstreamRepo
	accountRepo         *repo.AuthAccountRepo
	opRepo              proxyOperationStore
	backupRepo          proxyBackupStore
	agent               proxyAgent
	panelDir            string // /opt/nxpanel/nginx
	webUser             string
	webGroup            string
	dangerousCARoots    []string
	writeMu             sync.Mutex
	accessPolicyManaged func(string) bool
	accessPolicyPrepare AccessPolicyPrepare
	accessPolicyGuard   func(string, bool) error
}

type proxyBackupStore interface {
	CreateMany(context.Context, []*repo.Backup) error
}

type proxyOperationStore interface {
	Create(*repo.Operation) error
	UpdateErrorContext(context.Context, string, string, string, string, string) error
	UpdateStatusContext(context.Context, string, string) error
}

type proxyAgent interface {
	ReadFile(context.Context, string) ([]byte, string, error)
	ApplyTransaction(context.Context, *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
	FilesRemove(context.Context, []string) error
	FilesChown(context.Context, string, string, string, bool) error
	FilesChmod(context.Context, string, string, bool) error
}

// NewService 创建反向代理服务
func NewService(
	siteRepo *repo.SiteRepo,
	proxyRepo *repo.ProxyRepo,
	upstreamRepo *repo.UpstreamRepo,
	accountRepo *repo.AuthAccountRepo,
	opRepo proxyOperationStore,
	backupRepo *repo.BackupRepo,
	agent proxyAgent,
	cfg *app.Config,
) *Service {
	webGroup := cfg.Nginx.WebGroup
	if webGroup == "" {
		webGroup = cfg.Nginx.WebUser
	}
	dangerousCARoots := make([]string, 0, len(cfg.Nginx.AllowedRootPrefixes)+2)
	dangerousCARoots = append(dangerousCARoots, "/www/wwwroot", "/var/www")
	dangerousCARoots = append(dangerousCARoots, cfg.Nginx.AllowedRootPrefixes...)
	return &Service{
		siteRepo:         siteRepo,
		proxyRepo:        proxyRepo,
		upstreamRepo:     upstreamRepo,
		accountRepo:      accountRepo,
		opRepo:           opRepo,
		backupRepo:       backupRepo,
		agent:            agent,
		panelDir:         cfg.Nginx.PanelDir,
		webUser:          cfg.Nginx.WebUser,
		webGroup:         webGroup,
		dangerousCARoots: dangerousCARoots,
	}
}

// CreateProxyRequest 创建反代配置的请求参数
type CreateProxyRequest struct {
	Name                       string   `json:"name"`
	Enabled                    bool     `json:"enabled"`
	LocationPath               string   `json:"location_path"`
	UpstreamURL                string   `json:"upstream_url"`
	UpstreamID                 *string  `json:"upstream_id"`
	UpstreamScheme             string   `json:"upstream_scheme"`
	ProxySSLServerName         string   `json:"proxy_ssl_server_name"`
	ProxySSLVerify             *bool    `json:"proxy_ssl_verify"`
	ProxySSLTrustedCertificate string   `json:"proxy_ssl_trusted_certificate"`
	ProxySSLVerifyDepth        int      `json:"proxy_ssl_verify_depth"`
	HostHeader                 string   `json:"host_header"`
	WebSocketEnabled           bool     `json:"websocket_enabled"`
	ConnectTimeout             int      `json:"connect_timeout"`
	SendTimeout                int      `json:"send_timeout"`
	ReadTimeout                int      `json:"read_timeout"`
	CacheEnabled               bool     `json:"cache_enabled"`
	CacheType                  string   `json:"cache_type"`
	CacheTime                  int      `json:"cache_time"`
	AuthEnabled                bool     `json:"auth_enabled"`
	AuthAccountIDs             []string `json:"auth_account_ids"`
}

// UpdateProxyRequest 更新反代配置的请求参数
type UpdateProxyRequest struct {
	Name                       string   `json:"name"`
	Enabled                    bool     `json:"enabled"`
	LocationPath               string   `json:"location_path"`
	UpstreamURL                string   `json:"upstream_url"`
	UpstreamID                 *string  `json:"upstream_id"`
	UpstreamScheme             string   `json:"upstream_scheme"`
	ProxySSLServerName         string   `json:"proxy_ssl_server_name"`
	ProxySSLVerify             *bool    `json:"proxy_ssl_verify"`
	ProxySSLTrustedCertificate string   `json:"proxy_ssl_trusted_certificate"`
	ProxySSLVerifyDepth        int      `json:"proxy_ssl_verify_depth"`
	HostHeader                 string   `json:"host_header"`
	WebSocketEnabled           bool     `json:"websocket_enabled"`
	ConnectTimeout             int      `json:"connect_timeout"`
	SendTimeout                int      `json:"send_timeout"`
	ReadTimeout                int      `json:"read_timeout"`
	CacheEnabled               bool     `json:"cache_enabled"`
	CacheType                  string   `json:"cache_type"`
	CacheTime                  int      `json:"cache_time"`
	AuthEnabled                bool     `json:"auth_enabled"`
	AuthAccountIDs             []string `json:"auth_account_ids"`
}

// ProxyResponse 反代配置响应
type ProxyResponse struct {
	ID                         string                 `json:"id"`
	Name                       string                 `json:"name"`
	Enabled                    bool                   `json:"enabled"`
	LocationPath               string                 `json:"location_path"`
	UpstreamURL                string                 `json:"upstream_url"`
	UpstreamID                 *string                `json:"upstream_id"`
	UpstreamScheme             string                 `json:"upstream_scheme"`
	ProxySSLServerName         string                 `json:"proxy_ssl_server_name"`
	ProxySSLVerify             bool                   `json:"proxy_ssl_verify"`
	ProxySSLTrustedCertificate string                 `json:"proxy_ssl_trusted_certificate"`
	ProxySSLVerifyDepth        int                    `json:"proxy_ssl_verify_depth"`
	HostHeader                 string                 `json:"host_header"`
	WebSocketEnabled           bool                   `json:"websocket_enabled"`
	ConnectTimeout             int                    `json:"connect_timeout"`
	SendTimeout                int                    `json:"send_timeout"`
	ReadTimeout                int                    `json:"read_timeout"`
	CacheEnabled               bool                   `json:"cache_enabled"`
	CacheType                  string                 `json:"cache_type"`
	CacheTime                  int                    `json:"cache_time"`
	AuthEnabled                bool                   `json:"auth_enabled"`
	AuthAccountIDs             []string               `json:"auth_account_ids"`
	AuthAccounts               []*AuthAccountResponse `json:"auth_accounts"`
}

type AuthAccountResponse struct {
	ID       string `json:"id"`
	Scope    string `json:"scope"`
	SiteID   string `json:"site_id,omitempty"`
	Username string `json:"username"`
	Enabled  bool   `json:"enabled"`
}

// List 列出站点的所有反向代理配置
func (svc *Service) List(siteID string) ([]*ProxyResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	proxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	var responses []*ProxyResponse
	for _, p := range proxies {
		resp, err := svc.toProxyResponse(p)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		responses = append(responses, resp)
	}
	return responses, nil
}

// Get 获取单个反向代理配置
func (svc *Service) Get(siteID, proxyID string) (*ProxyResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	proxy, err := svc.proxyRepo.GetByID(proxyID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if proxy == nil || proxy.SiteID != siteID {
		return nil, app.NewAppError(app.ErrNotFound, "代理配置不存在", nil)
	}

	return svc.toProxyResponse(proxy)
}

// Create 创建反向代理配置
// 数据流：校验 → 保存 desired state → 应用 Nginx 配置
func (svc *Service) Create(ctx context.Context, siteID string, req *CreateProxyRequest, requestID string) (*ProxyResponse, string, error) {
	svc.writeMu.Lock()
	defer svc.writeMu.Unlock()
	if err := svc.guardAccessPolicyWrite(siteID, false); err != nil {
		return nil, "", err
	}
	if err := svc.rejectLegacyAuth(siteID, req.AuthEnabled, req.AuthAccountIDs); err != nil {
		return nil, "", err
	}

	if err := validateCreateRequest(req); err != nil {
		return nil, "", err
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	target, err := svc.resolveTarget(ctx, req.UpstreamID, req.UpstreamScheme, req.UpstreamURL,
		req.ProxySSLServerName, req.ProxySSLVerify, req.ProxySSLTrustedCertificate, req.ProxySSLVerifyDepth)
	if err != nil {
		return nil, "", err
	}

	// 路径冲突检测
	if err := svc.checkPathConflict(siteID, req.LocationPath, ""); err != nil {
		return nil, "", err
	}
	accounts, accountIDs, err := svc.validateAuthAccounts(siteID, req.AuthAccountIDs, req.AuthEnabled)
	if err != nil {
		return nil, "", err
	}

	proxy := &repo.SiteProxy{
		ID:                         app.NewOperationID(),
		SiteID:                     siteID,
		Name:                       req.Name,
		Enabled:                    req.Enabled,
		LocationPath:               req.LocationPath,
		UpstreamURL:                target.url,
		UpstreamID:                 target.id,
		UpstreamScheme:             target.scheme,
		ProxySSLServerName:         target.serverName,
		ProxySSLVerify:             target.verify,
		ProxySSLTrustedCertificate: target.trustedCertificate,
		ProxySSLVerifyDepth:        target.verifyDepth,
		HostHeader:                 req.HostHeader,
		WebSocketEnabled:           req.WebSocketEnabled,
		ConnectTimeout:             req.ConnectTimeout,
		SendTimeout:                req.SendTimeout,
		ReadTimeout:                req.ReadTimeout,
		CacheEnabled:               req.CacheEnabled,
		CacheType:                  req.CacheType,
		CacheTime:                  req.CacheTime,
		AuthEnabled:                req.AuthEnabled,
		AuthHtpasswdPath:           proxyHtpasswdPath(svc.panelDir, ""),
	}
	proxy.AuthHtpasswdPath = proxyHtpasswdPath(svc.panelDir, proxy.ID)

	if err := svc.proxyRepo.Create(proxy); err != nil {
		if isForeignKeyConstraint(err) {
			return nil, "", app.NewAppError(app.ErrValidationFailed, "引用的 upstream 不存在或已被删除", nil)
		}
		return nil, "", app.NewAppError(app.ErrInternalError, "创建反代配置失败: "+err.Error(), nil)
	}
	if err := svc.proxyRepo.SetAccountIDs(proxy.ID, accountIDs); err != nil {
		_ = svc.proxyRepo.Delete(proxy.ID)
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	allProxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, "", desiredSyncError(app.ErrInternalError, "读取代理配置失败: "+err.Error(), "", "not_attempted")
	}
	extraFiles := map[string]string{}
	if proxy.AuthEnabled {
		extraFiles[proxy.AuthHtpasswdPath] = renderHtpasswd(accounts)
	}
	opID, err := svc.applyNginxConfig(ctx, site, allProxies, "proxy.create", requestID, extraFiles)
	if err != nil {
		return nil, "", err
	}

	slog.Info("反代配置创建成功", "site_id", siteID, "proxy_id", proxy.ID, "operation_id", opID)
	resp, err := svc.toProxyResponse(proxy)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return resp, opID, nil
}

// Update 更新反向代理配置
// 数据流：校验 → 保存 desired state → 应用 Nginx 配置
func (svc *Service) Update(ctx context.Context, siteID, proxyID string, req *UpdateProxyRequest, requestID string) (*ProxyResponse, string, error) {
	svc.writeMu.Lock()
	defer svc.writeMu.Unlock()
	if err := svc.guardAccessPolicyWrite(siteID, false); err != nil {
		return nil, "", err
	}
	if err := svc.rejectLegacyAuth(siteID, req.AuthEnabled, req.AuthAccountIDs); err != nil {
		return nil, "", err
	}

	if err := validateUpdateRequest(req); err != nil {
		return nil, "", err
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	existing, err := svc.proxyRepo.GetByID(proxyID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if existing == nil || existing.SiteID != siteID {
		return nil, "", app.NewAppError(app.ErrNotFound, "代理配置不存在", nil)
	}
	target, err := svc.resolveTarget(ctx, req.UpstreamID, req.UpstreamScheme, req.UpstreamURL,
		req.ProxySSLServerName, req.ProxySSLVerify, req.ProxySSLTrustedCertificate, req.ProxySSLVerifyDepth)
	if err != nil {
		return nil, "", err
	}

	// 路径冲突检测（排除自己）
	if err := svc.checkPathConflict(siteID, req.LocationPath, proxyID); err != nil {
		return nil, "", err
	}
	accounts, accountIDs, err := svc.validateAuthAccounts(siteID, req.AuthAccountIDs, req.AuthEnabled)
	if err != nil {
		return nil, "", err
	}
	managedAuth := svc.accessPolicyOwnsAuth(siteID)
	if managedAuth {
		accountIDs, err = svc.proxyRepo.GetAccountIDs(existing.ID)
		if err != nil {
			return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
	}

	// 保存旧的缓存状态，用于判断是否需要清理
	oldProxy := *existing
	oldCacheEnabled := existing.CacheEnabled
	oldCacheType := existing.CacheType
	oldAuthHtpasswdPath := existing.AuthHtpasswdPath

	existing.Name = req.Name
	existing.Enabled = req.Enabled
	existing.LocationPath = req.LocationPath
	existing.UpstreamURL = target.url
	existing.UpstreamID = target.id
	existing.UpstreamScheme = target.scheme
	existing.ProxySSLServerName = target.serverName
	existing.ProxySSLVerify = target.verify
	existing.ProxySSLTrustedCertificate = target.trustedCertificate
	existing.ProxySSLVerifyDepth = target.verifyDepth
	existing.HostHeader = req.HostHeader
	existing.WebSocketEnabled = req.WebSocketEnabled
	existing.ConnectTimeout = req.ConnectTimeout
	existing.SendTimeout = req.SendTimeout
	existing.ReadTimeout = req.ReadTimeout
	existing.CacheEnabled = req.CacheEnabled
	existing.CacheType = req.CacheType
	existing.CacheTime = req.CacheTime
	if !managedAuth {
		existing.AuthEnabled = req.AuthEnabled
	}
	if existing.AuthHtpasswdPath == "" {
		existing.AuthHtpasswdPath = proxyHtpasswdPath(svc.panelDir, existing.ID)
	}

	if err := svc.proxyRepo.Update(existing); err != nil {
		if isForeignKeyConstraint(err) {
			return nil, "", app.NewAppError(app.ErrValidationFailed, "引用的 upstream 不存在或已被删除", nil)
		}
		return nil, "", app.NewAppError(app.ErrInternalError, "更新反代配置失败: "+err.Error(), nil)
	}
	if err := svc.proxyRepo.SetAccountIDs(existing.ID, accountIDs); err != nil {
		_ = svc.proxyRepo.Update(&oldProxy)
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	allProxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, "", desiredSyncError(app.ErrInternalError, "读取代理配置失败: "+err.Error(), "", "not_attempted")
	}
	extraFiles := map[string]string{}
	if existing.AuthEnabled && !managedAuth {
		extraFiles[existing.AuthHtpasswdPath] = renderHtpasswd(accounts)
	}
	opID, err := svc.applyNginxConfig(ctx, site, allProxies, "proxy.update", requestID, extraFiles)
	if err != nil {
		return nil, "", err
	}

	// 如果关闭了代理或关闭/切换了缓存，清理旧的缓存目录
	if oldCacheEnabled && (!req.Enabled || !req.CacheEnabled || oldCacheType != req.CacheType) {
		svc.cleanupCache(ctx, site, oldCacheType)
	}
	if !existing.AuthEnabled && oldAuthHtpasswdPath != "" {
		if err := svc.agent.FilesRemove(ctx, []string{oldAuthHtpasswdPath}); err != nil {
			slog.Warn("删除反代访问限制 htpasswd 失败", "error", err, "path", oldAuthHtpasswdPath)
		}
	}

	slog.Info("反代配置更新成功", "site_id", siteID, "proxy_id", proxyID, "operation_id", opID)
	resp, err := svc.toProxyResponse(existing)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return resp, opID, nil
}

// Delete 删除反向代理配置
// 数据流：删除 desired state → 应用剩余 Nginx 配置
func (svc *Service) Delete(ctx context.Context, siteID, proxyID string, requestID string) (string, error) {
	svc.writeMu.Lock()
	defer svc.writeMu.Unlock()
	if err := svc.guardAccessPolicyWrite(siteID, false); err != nil {
		return "", err
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	existing, err := svc.proxyRepo.GetByID(proxyID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if existing == nil || existing.SiteID != siteID {
		return "", app.NewAppError(app.ErrNotFound, "代理配置不存在", nil)
	}

	if err := svc.proxyRepo.Delete(proxyID); err != nil {
		return "", app.NewAppError(app.ErrInternalError, "删除反代配置失败: "+err.Error(), nil)
	}
	remainingProxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return "", desiredSyncError(app.ErrInternalError, "读取代理配置失败: "+err.Error(), "", "not_attempted")
	}

	opID, err := svc.applyNginxConfig(ctx, site, remainingProxies, "proxy.delete", requestID, nil)
	if err != nil {
		return opID, err
	}

	// 确认没有其他代理使用相同缓存类型后再清理
	stillUsesSameCache := false
	for _, p := range remainingProxies {
		if p.CacheEnabled && p.CacheType == existing.CacheType {
			stillUsesSameCache = true
			break
		}
	}
	if existing.CacheEnabled && !stillUsesSameCache {
		svc.cleanupCache(ctx, site, existing.CacheType)
	}
	if existing.AuthHtpasswdPath != "" {
		if err := svc.agent.FilesRemove(ctx, []string{existing.AuthHtpasswdPath}); err != nil {
			slog.Warn("删除反代访问限制 htpasswd 失败", "error", err, "path", existing.AuthHtpasswdPath)
		}
	}

	slog.Info("反代配置删除成功", "site_id", siteID, "proxy_id", proxyID, "operation_id", opID)
	return opID, nil
}

// Sync rebuilds the site proxy markers and auth files from persisted desired state.
func (svc *Service) Sync(ctx context.Context, siteID, requestID string) (string, error) {
	svc.writeMu.Lock()
	defer svc.writeMu.Unlock()
	if err := svc.guardAccessPolicyWrite(siteID, true); err != nil {
		return "", err
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	proxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return "", desiredSyncError(app.ErrInternalError, "读取代理配置失败: "+err.Error(), "", "not_attempted")
	}
	extraFiles := make(map[string]string)
	for _, proxy := range proxies {
		if !proxy.AuthEnabled || svc.accessPolicyOwnsAuth(siteID) {
			continue
		}
		if proxy.AuthHtpasswdPath == "" {
			proxy.AuthHtpasswdPath = proxyHtpasswdPath(svc.panelDir, proxy.ID)
		}
		accounts, err := svc.accountsForProxy(proxy.ID)
		if err != nil {
			return "", desiredSyncError(app.ErrInternalError, "读取反代访问账户失败: "+err.Error(), "", "not_attempted")
		}
		extraFiles[proxy.AuthHtpasswdPath] = renderHtpasswd(accounts)
	}
	return svc.applyNginxConfig(ctx, site, proxies, "proxy.sync", requestID, extraFiles)
}

// applyNginxConfig 应用 Nginx 配置
func (svc *Service) applyNginxConfig(ctx context.Context, site *repo.Site, proxies []*repo.SiteProxy, action, requestID string, extraFiles map[string]string) (string, error) {
	opID := app.NewOperationID()
	if err := svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: action, TargetType: "site", TargetID: site.ID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("更新站点 %s 反代配置", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return "", desiredSyncError(app.ErrInternalError, "创建操作记录失败: "+err.Error(), "", "not_attempted")
	}

	// 读取当前配置
	currentContent, _, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return opID, svc.failConfigApply(ctx, opID, app.ErrAgentUnavailable, fmt.Errorf("读取配置文件失败: %w", err), "not_attempted")
	}

	// 构建渲染数据
	data := &nginx.RenderData{
		Proxies: make([]*nginx.ProxyData, 0, len(proxies)),
	}
	for _, p := range proxies {
		data.Proxies = append(data.Proxies, &nginx.ProxyData{
			ID:                         p.ID,
			Name:                       p.Name,
			Enabled:                    p.Enabled,
			LocationPath:               p.LocationPath,
			UpstreamURL:                p.UpstreamURL,
			ManagedUpstream:            p.UpstreamID != nil,
			UpstreamScheme:             p.UpstreamScheme,
			ProxySSLServerName:         p.ProxySSLServerName,
			ProxySSLVerify:             p.ProxySSLVerify,
			ProxySSLTrustedCertificate: p.ProxySSLTrustedCertificate,
			ProxySSLVerifyDepth:        p.ProxySSLVerifyDepth,
			HostHeader:                 p.HostHeader,
			WebSocketEnabled:           p.WebSocketEnabled,
			ConnectTimeout:             p.ConnectTimeout,
			SendTimeout:                p.SendTimeout,
			ReadTimeout:                p.ReadTimeout,
			CacheEnabled:               p.CacheEnabled,
			CacheType:                  p.CacheType,
			CacheTime:                  p.CacheTime,
			CachePath:                  site.RootPath + "/.cache/proxy",
			AuthEnabled:                p.AuthEnabled && !svc.accessPolicyOwnsAuth(site.ID),
			AuthHtpasswdPath:           p.AuthHtpasswdPath,
		})
	}

	mainLocation := nginx.BuildMainLocation(data)
	extraLocations := nginx.BuildExtraLocations(data)
	patched, err := nginx.ApplyOptionalMarkerPatches(currentContent, []nginx.BlockPatch{
		{Name: nginx.MarkerNameMainLocation, Body: []byte(mainLocation)},
		{Name: nginx.MarkerNameExtraLocations, Body: []byte(extraLocations)},
	})
	if err != nil {
		return opID, svc.failConfigApply(ctx, opID, app.ErrInternalError, fmt.Errorf("反代标识块更新失败: %w", err), "not_attempted")
	}

	// 准备文件变更列表
	changes := []agentclient.FileChangeRequest{
		{
			Type:          "write",
			Path:          site.ConfigPath,
			ContentBase64: base64.StdEncoding.EncodeToString(patched),
			Perm:          0644,
		},
	}

	// 检查是否需要创建 Nginx 缓存目录和配置
	needNginxCache := false
	for _, p := range proxies {
		if p.CacheEnabled && p.CacheType == "nginx" && p.Enabled {
			needNginxCache = true
			break
		}
	}

	proxyCacheConfPath := filepath.Join(svc.panelDir, "conf.d", "proxy-cache.conf")
	if needNginxCache {
		// 创建 Nginx 缓存目录
		cacheDir := filepath.Join(svc.panelDir, "proxy", "cache")
		changes = append(changes, agentclient.FileChangeRequest{
			Type: "mkdir",
			Path: cacheDir,
			Perm: 0755,
		})

		// 创建/更新 conf.d/proxy-cache.conf
		proxyCacheContent := nginx.BuildProxyCacheConf(cacheDir)
		changes = append(changes, agentclient.FileChangeRequest{
			Type:          "write",
			Path:          proxyCacheConfPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(proxyCacheContent)),
			Perm:          0644,
		})
	} else {
		// 没有代理需要 Nginx 缓存，移除 conf.d/proxy-cache.conf
		changes = append(changes, agentclient.FileChangeRequest{
			Type: "remove",
			Path: proxyCacheConfPath,
		})
	}

	// 文件缓存目录由事务内的 mkdir 创建（确保 nginx -t 时 proxy_temp_path 存在），
	// 事务后的 FilesMkdir RPC 负责 chown（agent 自动 applyWebOwner）
	for _, p := range proxies {
		if p.CacheEnabled && p.CacheType == "file" && p.Enabled {
			cacheDir := filepath.Join(site.RootPath, ".cache", "proxy")
			changes = append(changes, agentclient.FileChangeRequest{
				Type: "mkdir",
				Path: cacheDir,
				Perm: 0755,
			})
			break
		}
	}
	for path, content := range extraFiles {
		if strings.TrimSpace(path) == "" {
			continue
		}
		changes = append(changes, agentclient.FileChangeRequest{Type: "mkdir", Path: filepath.Dir(path), Perm: 0755})
		changes = append(changes, agentclient.FileChangeRequest{
			Type:          "write",
			Path:          path,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)),
			Perm:          0644,
		})
	}

	var finishPolicy func(error) error
	if svc.accessPolicyOwnsAuth(site.ID) && svc.accessPolicyPrepare != nil {
		changes, finishPolicy, err = svc.accessPolicyPrepare(ctx, site, proxies, changes)
		if err != nil {
			return opID, svc.failConfigApply(ctx, opID, app.ErrInternalError, fmt.Errorf("准备访问策略失败: %w", err), "not_attempted")
		}
	}
	// 通过 agent 写入文件
	result, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if finishPolicy != nil {
		policyErr := agentErr
		if policyErr == nil && result == nil {
			policyErr = fmt.Errorf("Agent 返回空事务结果")
		}
		if finishErr := finishPolicy(policyErr); finishErr != nil && agentErr == nil {
			return opID, svc.failConfigApply(ctx, opID, app.ErrInternalError, fmt.Errorf("保存访问策略应用状态失败: %w", finishErr), "applied")
		}
	}
	if agentErr != nil {
		return opID, svc.failConfigApply(ctx, opID, app.ErrAgentUnavailable, fmt.Errorf("文件事务失败: %w", agentErr), "unknown")
	}
	if result == nil {
		return opID, svc.failConfigApply(ctx, opID, app.ErrInternalError, fmt.Errorf("Agent 返回空事务结果"), "applied")
	}
	backups := make([]*repo.Backup, 0, len(result.Backups))
	for _, item := range result.Backups {
		backups = append(backups, &repo.Backup{
			ID: app.NewID("backup"), OperationID: opID, FilePath: item.FilePath,
			BackupPath: item.BackupPath, FileExisted: item.Existed,
		})
	}
	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err = svc.backupRepo.CreateMany(recordCtx, backups)
	cancelRecord()
	if err != nil {
		return opID, svc.failConfigApply(ctx, opID, app.ErrInternalError, fmt.Errorf("保存事务备份记录失败: %w", err), "applied")
	}

	// 事务成功后，设置文件缓存目录所有者（确保 nginx 可读写）
	for _, p := range proxies {
		if p.CacheEnabled && p.CacheType == "file" && p.Enabled {
			cacheDir := filepath.Join(site.RootPath, ".cache")
			if svc.webUser != "" {
				if err := svc.agent.FilesChown(ctx, cacheDir, svc.webUser, svc.webGroup, true); err != nil {
					slog.Warn("设置文件缓存目录所有者失败", "error", err, "path", cacheDir)
				}
			} else {
				if err := svc.agent.FilesChmod(ctx, cacheDir, "0777", true); err != nil {
					slog.Warn("设置文件缓存目录权限失败", "error", err, "path", cacheDir)
				}
			}
			break
		}
	}

	finalCtx, cancelFinal := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err = svc.opRepo.UpdateStatusContext(finalCtx, opID, "success")
	cancelFinal()
	if err != nil {
		return opID, operationTerminalError(opID, fmt.Errorf("反代配置已应用"), err, "applied", false)
	}
	return opID, nil
}

func (svc *Service) failConfigApply(ctx context.Context, operationID, code string, cause error, outcome string) error {
	finalCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	terminalErr := svc.opRepo.UpdateErrorContext(finalCtx, operationID, "failed", code, cause.Error(), "")
	cancel()
	if terminalErr != nil {
		return operationTerminalError(operationID, cause, terminalErr, outcome, true)
	}
	return desiredSyncError(code, cause.Error(), operationID, outcome)
}

func desiredSyncError(code, message, operationID, outcome string) *app.AppError {
	details := map[string]any{"desired_saved": true, "sync_required": true}
	if operationID != "" {
		details["operation_id"] = operationID
	}
	setConfigOutcome(details, outcome)
	return app.NewAppError(code, message, details)
}

func operationTerminalError(operationID string, cause, terminalErr error, outcome string, syncRequired bool) *app.AppError {
	details := map[string]any{
		"operation_id": operationID, "desired_saved": true,
		"sync_required": syncRequired, "operation_update_failed": true,
	}
	setConfigOutcome(details, outcome)
	return app.NewAppError(app.ErrInternalError,
		fmt.Sprintf("%v; 更新 operation 终态失败: %v", cause, terminalErr), details)
}

func setConfigOutcome(details map[string]any, outcome string) {
	if outcome == "applied" {
		details["config_applied"] = true
		return
	}
	details["config_apply_outcome"] = outcome
}

// cleanupCache 清理缓存
func (svc *Service) cleanupCache(ctx context.Context, site *repo.Site, cacheType string) {
	switch cacheType {
	case "nginx":
		cacheDir := filepath.Join(svc.panelDir, "proxy", "cache")
		if err := svc.agent.FilesRemove(ctx, []string{cacheDir}); err != nil {
			slog.Warn("删除 Nginx 缓存目录失败", "error", err, "path", cacheDir)
		} else {
			slog.Info("Nginx 缓存目录已删除", "path", cacheDir)
		}
	case "file":
		cacheDir := filepath.Join(site.RootPath, ".cache")
		if err := svc.agent.FilesRemove(ctx, []string{cacheDir}); err != nil {
			slog.Warn("删除文件缓存目录失败", "error", err, "path", cacheDir)
		} else {
			slog.Info("文件缓存目录已删除", "path", cacheDir)
		}
	}
}

// checkPathConflict 检查路径冲突
func (svc *Service) checkPathConflict(siteID, locationPath, excludeID string) error {
	conflict, err := svc.proxyRepo.CheckPathConflict(siteID, locationPath, excludeID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, "检测路径冲突失败: "+err.Error(), nil)
	}
	if conflict {
		return app.NewAppError(app.ErrValidationFailed, "代理路径与其他代理冲突", nil)
	}
	return nil
}

// validateCreateRequest 校验创建请求
func validateCreateRequest(req *CreateProxyRequest) *app.AppError {
	return validateProxyFields(&req.Name, &req.LocationPath, &req.HostHeader,
		&req.ConnectTimeout, &req.SendTimeout, &req.ReadTimeout, req.CacheEnabled, req.CacheType, req.CacheTime)
}

// validateUpdateRequest 校验更新请求
func validateUpdateRequest(req *UpdateProxyRequest) *app.AppError {
	return validateProxyFields(&req.Name, &req.LocationPath, &req.HostHeader,
		&req.ConnectTimeout, &req.SendTimeout, &req.ReadTimeout, req.CacheEnabled, req.CacheType, req.CacheTime)
}

// validateProxyFields 校验代理字段
func validateProxyFields(name, locationPath, hostHeader *string,
	connectTimeout, sendTimeout, readTimeout *int, cacheEnabled bool, cacheType string, cacheTime int) *app.AppError {

	// 校验代理名称
	if *name == "" {
		return app.NewAppError(app.ErrValidationFailed, "代理名称不能为空", nil)
	}

	// 校验路径
	if *locationPath == "" {
		*locationPath = "/"
	}
	if err := security.ValidateProxyLocationPath(*locationPath); err != nil {
		return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}

	// 校验 host header
	if *hostHeader == "" {
		*hostHeader = "$host"
	}
	if err := security.ValidateHostHeader(*hostHeader); err != nil {
		return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}

	// 校验 timeout 范围（1-3600 秒）
	if *connectTimeout == 0 {
		*connectTimeout = 60
	}
	if *connectTimeout < 1 || *connectTimeout > 3600 {
		return app.NewAppError(app.ErrValidationFailed, "connect_timeout 必须在 1-3600 秒之间", nil)
	}
	if *sendTimeout == 0 {
		*sendTimeout = 60
	}
	if *sendTimeout < 1 || *sendTimeout > 3600 {
		return app.NewAppError(app.ErrValidationFailed, "send_timeout 必须在 1-3600 秒之间", nil)
	}
	if *readTimeout == 0 {
		*readTimeout = 60
	}
	if *readTimeout < 1 || *readTimeout > 3600 {
		return app.NewAppError(app.ErrValidationFailed, "read_timeout 必须在 1-3600 秒之间", nil)
	}

	// 校验缓存配置
	if cacheEnabled {
		if cacheType != "nginx" && cacheType != "file" {
			return app.NewAppError(app.ErrValidationFailed, "缓存类型必须为 nginx 或 file", nil)
		}
		if cacheTime <= 0 {
			return app.NewAppError(app.ErrValidationFailed, "缓存时间必须大于 0", nil)
		}
	}

	return nil
}

type resolvedTarget struct {
	id                 *string
	url                string
	scheme             string
	serverName         string
	verify             bool
	trustedCertificate string
	verifyDepth        int
}

func (svc *Service) resolveTarget(ctx context.Context, upstreamID *string, scheme, directURL, serverName string,
	verify *bool, trustedCertificate string, verifyDepth int) (*resolvedTarget, error) {
	if upstreamID == nil || strings.TrimSpace(*upstreamID) == "" {
		if verify != nil || strings.TrimSpace(serverName) != "" || strings.TrimSpace(trustedCertificate) != "" || verifyDepth != 0 {
			return nil, app.NewAppError(app.ErrValidationFailed, "direct 模式不接受托管 upstream TLS 字段", nil)
		}
		directURL = strings.TrimSpace(directURL)
		if err := security.ValidateUpstreamURL(directURL); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
		return &resolvedTarget{url: directURL, scheme: "http"}, nil
	}

	id := strings.TrimSpace(*upstreamID)
	if strings.TrimSpace(directURL) != "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "托管 upstream 模式不接受 upstream_url", nil)
	}
	upstream, err := svc.upstreamRepo.GetByID(ctx, id)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, "查询 upstream 失败: "+err.Error(), nil)
	}
	if upstream == nil {
		return nil, app.NewAppError(app.ErrValidationFailed, "引用的 upstream 不存在", nil)
	}
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme == "" {
		scheme = "http"
	}
	if scheme != "http" && scheme != "https" {
		return nil, app.NewAppError(app.ErrValidationFailed, "upstream_scheme 必须为 http 或 https", nil)
	}
	target := &resolvedTarget{id: &id, url: scheme + "://" + upstream.Name, scheme: scheme}
	if scheme == "http" {
		return target, nil
	}

	target.serverName = strings.TrimSpace(serverName)
	if err := security.ValidateProxySSLServerName(target.serverName); err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	target.verify = verify == nil || *verify
	if !target.verify {
		return target, nil
	}
	target.trustedCertificate = strings.TrimSpace(trustedCertificate)
	if err := security.ValidateProxySSLTrustedCertificate(target.trustedCertificate); err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	if root, denied := svc.dangerousCARoot(target.trustedCertificate); denied {
		return nil, app.NewAppError(app.ErrValidationFailed,
			"proxy_ssl_trusted_certificate 不能位于网站根目录，避免 Web 用户替换信任根",
			map[string]any{"path": target.trustedCertificate, "denied_root": root})
	}
	if verifyDepth == 0 {
		verifyDepth = 3
	}
	if verifyDepth < 1 || verifyDepth > 100 {
		return nil, app.NewAppError(app.ErrValidationFailed, "proxy_ssl_verify_depth 必须在 1-100 之间", nil)
	}
	if _, _, err := svc.agent.ReadFile(ctx, target.trustedCertificate); err != nil {
		if app.IsPathDeniedError(err) {
			return nil, app.NewPathDeniedError("读取上游可信 CA", "证书文件", target.trustedCertificate)
		}
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取上游可信 CA 失败: "+err.Error(), nil)
	}
	target.verifyDepth = verifyDepth
	return target, nil
}

func (svc *Service) dangerousCARoot(path string) (string, bool) {
	for _, root := range svc.dangerousCARoots {
		if pathWithinDirectory(path, root) {
			return filepath.Clean(root), true
		}
	}
	return "", false
}

func pathWithinDirectory(path, root string) bool {
	if !filepath.IsAbs(path) || !filepath.IsAbs(root) {
		return false
	}
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isForeignKeyConstraint(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "foreign key constraint")
}

// toProxyResponse 转换为响应结构
func (svc *Service) toProxyResponse(p *repo.SiteProxy) (*ProxyResponse, error) {
	accounts, err := svc.accountsForProxy(p.ID)
	if err != nil {
		return nil, err
	}
	accountIDs := make([]string, 0, len(accounts))
	accountResponses := make([]*AuthAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
		accountResponses = append(accountResponses, &AuthAccountResponse{
			ID:       account.ID,
			Scope:    account.Scope,
			SiteID:   account.SiteID,
			Username: account.Username,
			Enabled:  account.Enabled,
		})
	}
	return &ProxyResponse{
		ID:                         p.ID,
		Name:                       p.Name,
		Enabled:                    p.Enabled,
		LocationPath:               p.LocationPath,
		UpstreamURL:                p.UpstreamURL,
		UpstreamID:                 p.UpstreamID,
		UpstreamScheme:             p.UpstreamScheme,
		ProxySSLServerName:         p.ProxySSLServerName,
		ProxySSLVerify:             p.ProxySSLVerify,
		ProxySSLTrustedCertificate: p.ProxySSLTrustedCertificate,
		ProxySSLVerifyDepth:        p.ProxySSLVerifyDepth,
		HostHeader:                 p.HostHeader,
		WebSocketEnabled:           p.WebSocketEnabled,
		ConnectTimeout:             p.ConnectTimeout,
		SendTimeout:                p.SendTimeout,
		ReadTimeout:                p.ReadTimeout,
		CacheEnabled:               p.CacheEnabled,
		CacheType:                  p.CacheType,
		CacheTime:                  p.CacheTime,
		AuthEnabled:                p.AuthEnabled,
		AuthAccountIDs:             accountIDs,
		AuthAccounts:               accountResponses,
	}, nil
}

func (svc *Service) validateAuthAccounts(siteID string, accountIDs []string, enabled bool) ([]*repo.AuthAccount, []string, error) {
	if !enabled {
		return nil, nil, nil
	}
	seen := make(map[string]struct{}, len(accountIDs))
	ids := make([]string, 0, len(accountIDs))
	for _, raw := range accountIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil, app.NewAppError(app.ErrValidationFailed, "开启访问限制时请选择至少一个账户", nil)
	}
	accounts, err := svc.accountRepo.ListByIDs(ids)
	if err != nil {
		return nil, nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if len(accounts) != len(ids) {
		return nil, nil, app.NewAppError(app.ErrValidationFailed, "包含不存在的账户", nil)
	}
	for _, account := range accounts {
		if account.Scope != "global" && account.SiteID != siteID {
			return nil, nil, app.NewAppError(app.ErrValidationFailed, "包含不可用于当前站点的账户", nil)
		}
		if !account.Enabled {
			return nil, nil, app.NewAppError(app.ErrValidationFailed, "不能选择已禁用账户", nil)
		}
	}
	return accounts, ids, nil
}

func (svc *Service) accountsForProxy(proxyID string) ([]*repo.AuthAccount, error) {
	ids, err := svc.proxyRepo.GetAccountIDs(proxyID)
	if err != nil {
		return nil, err
	}
	return svc.accountRepo.ListByIDs(ids)
}

func renderHtpasswd(accounts []*repo.AuthAccount) string {
	entries := make([]string, 0, len(accounts))
	for _, account := range accounts {
		if account.Enabled {
			entries = append(entries, account.PasswordHash)
		}
	}
	if len(entries) == 0 {
		return ""
	}
	return strings.Join(entries, "\n") + "\n"
}

func proxyHtpasswdPath(panelDir, proxyID string) string {
	if proxyID == "" {
		return ""
	}
	return filepath.Join(panelDir, "htpasswd", "proxy", proxyID+".htpasswd")
}
