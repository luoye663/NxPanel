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
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/security"
)

// Service 反向代理业务服务
type Service struct {
	siteRepo    *repo.SiteRepo
	proxyRepo   *repo.ProxyRepo
	accountRepo *repo.AuthAccountRepo
	opRepo      *repo.OperationRepo
	agent       *agentclient.Client
	panelDir    string // /opt/nxpanel/nginx
	webUser     string
	webGroup    string
}

// NewService 创建反向代理服务
func NewService(
	siteRepo *repo.SiteRepo,
	proxyRepo *repo.ProxyRepo,
	accountRepo *repo.AuthAccountRepo,
	opRepo *repo.OperationRepo,
	agent *agentclient.Client,
	cfg *app.Config,
) *Service {
	webGroup := cfg.Nginx.WebGroup
	if webGroup == "" {
		webGroup = cfg.Nginx.WebUser
	}
	return &Service{
		siteRepo:    siteRepo,
		proxyRepo:   proxyRepo,
		accountRepo: accountRepo,
		opRepo:      opRepo,
		agent:       agent,
		panelDir:    cfg.Nginx.PanelDir,
		webUser:     cfg.Nginx.WebUser,
		webGroup:    webGroup,
	}
}

// CreateProxyRequest 创建反代配置的请求参数
type CreateProxyRequest struct {
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	LocationPath     string   `json:"location_path"`
	UpstreamURL      string   `json:"upstream_url"`
	HostHeader       string   `json:"host_header"`
	WebSocketEnabled bool     `json:"websocket_enabled"`
	ConnectTimeout   int      `json:"connect_timeout"`
	SendTimeout      int      `json:"send_timeout"`
	ReadTimeout      int      `json:"read_timeout"`
	CacheEnabled     bool     `json:"cache_enabled"`
	CacheType        string   `json:"cache_type"`
	CacheTime        int      `json:"cache_time"`
	AuthEnabled      bool     `json:"auth_enabled"`
	AuthAccountIDs   []string `json:"auth_account_ids"`
}

// UpdateProxyRequest 更新反代配置的请求参数
type UpdateProxyRequest struct {
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	LocationPath     string   `json:"location_path"`
	UpstreamURL      string   `json:"upstream_url"`
	HostHeader       string   `json:"host_header"`
	WebSocketEnabled bool     `json:"websocket_enabled"`
	ConnectTimeout   int      `json:"connect_timeout"`
	SendTimeout      int      `json:"send_timeout"`
	ReadTimeout      int      `json:"read_timeout"`
	CacheEnabled     bool     `json:"cache_enabled"`
	CacheType        string   `json:"cache_type"`
	CacheTime        int      `json:"cache_time"`
	AuthEnabled      bool     `json:"auth_enabled"`
	AuthAccountIDs   []string `json:"auth_account_ids"`
}

// ProxyResponse 反代配置响应
type ProxyResponse struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Enabled          bool                   `json:"enabled"`
	LocationPath     string                 `json:"location_path"`
	UpstreamURL      string                 `json:"upstream_url"`
	HostHeader       string                 `json:"host_header"`
	WebSocketEnabled bool                   `json:"websocket_enabled"`
	ConnectTimeout   int                    `json:"connect_timeout"`
	SendTimeout      int                    `json:"send_timeout"`
	ReadTimeout      int                    `json:"read_timeout"`
	CacheEnabled     bool                   `json:"cache_enabled"`
	CacheType        string                 `json:"cache_type"`
	CacheTime        int                    `json:"cache_time"`
	AuthEnabled      bool                   `json:"auth_enabled"`
	AuthAccountIDs   []string               `json:"auth_account_ids"`
	AuthAccounts     []*AuthAccountResponse `json:"auth_accounts"`
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
// 数据流：校验 → 应用 Nginx 配置 → 入库
func (svc *Service) Create(ctx context.Context, siteID string, req *CreateProxyRequest, requestID string) (*ProxyResponse, string, error) {
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

	// 路径冲突检测
	if err := svc.checkPathConflict(siteID, req.LocationPath, ""); err != nil {
		return nil, "", err
	}
	accounts, accountIDs, err := svc.validateAuthAccounts(siteID, req.AuthAccountIDs, req.AuthEnabled)
	if err != nil {
		return nil, "", err
	}

	proxy := &repo.SiteProxy{
		ID:               app.NewOperationID(),
		SiteID:           siteID,
		Name:             req.Name,
		Enabled:          req.Enabled,
		LocationPath:     req.LocationPath,
		UpstreamURL:      req.UpstreamURL,
		HostHeader:       req.HostHeader,
		WebSocketEnabled: req.WebSocketEnabled,
		ConnectTimeout:   req.ConnectTimeout,
		SendTimeout:      req.SendTimeout,
		ReadTimeout:      req.ReadTimeout,
		CacheEnabled:     req.CacheEnabled,
		CacheType:        req.CacheType,
		CacheTime:        req.CacheTime,
		AuthEnabled:      req.AuthEnabled,
		AuthHtpasswdPath: proxyHtpasswdPath(svc.panelDir, ""),
	}
	proxy.AuthHtpasswdPath = proxyHtpasswdPath(svc.panelDir, proxy.ID)

	if err := svc.proxyRepo.Create(proxy); err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "创建反代配置失败: "+err.Error(), nil)
	}
	if err := svc.proxyRepo.SetAccountIDs(proxy.ID, accountIDs); err != nil {
		_ = svc.proxyRepo.Delete(proxy.ID)
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	allProxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		_ = svc.proxyRepo.Delete(proxy.ID)
		return nil, "", app.NewAppError(app.ErrInternalError, "读取代理配置失败: "+err.Error(), nil)
	}
	extraFiles := map[string]string{}
	if proxy.AuthEnabled {
		extraFiles[proxy.AuthHtpasswdPath] = renderHtpasswd(accounts)
	}
	opID, err := svc.applyNginxConfig(ctx, site, allProxies, "proxy.create", extraFiles)
	if err != nil {
		_ = svc.proxyRepo.Delete(proxy.ID)
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
// 数据流：校验 → 应用 Nginx 配置 → 入库
func (svc *Service) Update(ctx context.Context, siteID, proxyID string, req *UpdateProxyRequest, requestID string) (*ProxyResponse, string, error) {
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

	// 路径冲突检测（排除自己）
	if err := svc.checkPathConflict(siteID, req.LocationPath, proxyID); err != nil {
		return nil, "", err
	}
	accounts, accountIDs, err := svc.validateAuthAccounts(siteID, req.AuthAccountIDs, req.AuthEnabled)
	if err != nil {
		return nil, "", err
	}

	// 保存旧的缓存状态，用于判断是否需要清理
	oldProxy := *existing
	oldAccountIDs, err := svc.proxyRepo.GetAccountIDs(existing.ID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	oldCacheEnabled := existing.CacheEnabled
	oldCacheType := existing.CacheType
	oldAuthHtpasswdPath := existing.AuthHtpasswdPath

	existing.Name = req.Name
	existing.Enabled = req.Enabled
	existing.LocationPath = req.LocationPath
	existing.UpstreamURL = req.UpstreamURL
	existing.HostHeader = req.HostHeader
	existing.WebSocketEnabled = req.WebSocketEnabled
	existing.ConnectTimeout = req.ConnectTimeout
	existing.SendTimeout = req.SendTimeout
	existing.ReadTimeout = req.ReadTimeout
	existing.CacheEnabled = req.CacheEnabled
	existing.CacheType = req.CacheType
	existing.CacheTime = req.CacheTime
	existing.AuthEnabled = req.AuthEnabled
	if existing.AuthHtpasswdPath == "" {
		existing.AuthHtpasswdPath = proxyHtpasswdPath(svc.panelDir, existing.ID)
	}

	if err := svc.proxyRepo.Update(existing); err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "更新反代配置失败: "+err.Error(), nil)
	}
	if err := svc.proxyRepo.SetAccountIDs(existing.ID, accountIDs); err != nil {
		_ = svc.proxyRepo.Update(&oldProxy)
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	allProxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		_ = svc.proxyRepo.Update(&oldProxy)
		_ = svc.proxyRepo.SetAccountIDs(existing.ID, oldAccountIDs)
		return nil, "", app.NewAppError(app.ErrInternalError, "读取代理配置失败: "+err.Error(), nil)
	}
	extraFiles := map[string]string{}
	if existing.AuthEnabled {
		extraFiles[existing.AuthHtpasswdPath] = renderHtpasswd(accounts)
	}
	opID, err := svc.applyNginxConfig(ctx, site, allProxies, "proxy.update", extraFiles)
	if err != nil {
		_ = svc.proxyRepo.Update(&oldProxy)
		_ = svc.proxyRepo.SetAccountIDs(existing.ID, oldAccountIDs)
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
// 数据流：应用 Nginx 配置 → 删除入库
func (svc *Service) Delete(ctx context.Context, siteID, proxyID string, requestID string) (string, error) {
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

	// 获取剩余的代理配置（排除要删除的）
	allProxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, "读取代理配置失败: "+err.Error(), nil)
	}
	var remainingProxies []*repo.SiteProxy
	for _, p := range allProxies {
		if p.ID != proxyID {
			remainingProxies = append(remainingProxies, p)
		}
	}

	// 先应用 Nginx 配置（使用剩余的代理）
	opID, err := svc.applyNginxConfig(ctx, site, remainingProxies, "proxy.delete", nil)
	if err != nil {
		return "", err
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

	// 成功后再删除入库
	if err := svc.proxyRepo.Delete(proxyID); err != nil {
		return "", app.NewAppError(app.ErrInternalError, "删除反代配置失败: "+err.Error(), nil)
	}

	slog.Info("反代配置删除成功", "site_id", siteID, "proxy_id", proxyID, "operation_id", opID)
	return opID, nil
}

// applyNginxConfig 应用 Nginx 配置
func (svc *Service) applyNginxConfig(ctx context.Context, site *repo.Site, proxies []*repo.SiteProxy, action string, extraFiles map[string]string) (string, error) {
	// 读取当前配置
	currentContent, _, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return "", app.NewAppError(app.ErrAgentUnavailable, "读取配置文件失败: "+err.Error(), nil)
	}

	// 构建渲染数据
	data := &nginx.RenderData{
		Proxies: make([]*nginx.ProxyData, 0, len(proxies)),
	}
	for _, p := range proxies {
		data.Proxies = append(data.Proxies, &nginx.ProxyData{
			ID:               p.ID,
			Name:             p.Name,
			Enabled:          p.Enabled,
			LocationPath:     p.LocationPath,
			UpstreamURL:      p.UpstreamURL,
			HostHeader:       p.HostHeader,
			WebSocketEnabled: p.WebSocketEnabled,
			ConnectTimeout:   p.ConnectTimeout,
			SendTimeout:      p.SendTimeout,
			ReadTimeout:      p.ReadTimeout,
			CacheEnabled:     p.CacheEnabled,
			CacheType:        p.CacheType,
			CacheTime:        p.CacheTime,
			CachePath:        site.RootPath + "/.cache/proxy",
			AuthEnabled:      p.AuthEnabled,
			AuthHtpasswdPath: p.AuthHtpasswdPath,
		})
	}

	mainLocation := nginx.BuildMainLocation(data)
	extraLocations := nginx.BuildExtraLocations(data)
	patched, err := nginx.ApplyOptionalMarkerPatches(currentContent, []nginx.BlockPatch{
		{Name: nginx.MarkerNameMainLocation, Body: []byte(mainLocation)},
		{Name: nginx.MarkerNameExtraLocations, Body: []byte(extraLocations)},
	})
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, "反代标识块更新失败: "+err.Error(), nil)
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

	// 创建操作记录
	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: action, TargetType: "site", TargetID: site.ID,
		Status: "pending", RequestID: "", Actor: "admin",
		Message:   fmt.Sprintf("更新站点 %s 反代配置", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// 通过 agent 写入文件
	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return "", app.NewAppError(app.ErrAgentUnavailable, "文件事务失败: "+agentErr.Error(), nil)
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

	_ = svc.opRepo.UpdateStatus(opID, "success")
	return opID, nil
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
	return validateProxyFields(&req.Name, &req.LocationPath, &req.UpstreamURL, &req.HostHeader,
		req.ConnectTimeout, req.SendTimeout, req.ReadTimeout, req.CacheEnabled, req.CacheType, req.CacheTime)
}

// validateUpdateRequest 校验更新请求
func validateUpdateRequest(req *UpdateProxyRequest) *app.AppError {
	return validateProxyFields(&req.Name, &req.LocationPath, &req.UpstreamURL, &req.HostHeader,
		req.ConnectTimeout, req.SendTimeout, req.ReadTimeout, req.CacheEnabled, req.CacheType, req.CacheTime)
}

// validateProxyFields 校验代理字段
func validateProxyFields(name, locationPath, upstreamURL, hostHeader *string,
	connectTimeout, sendTimeout, readTimeout int, cacheEnabled bool, cacheType string, cacheTime int) *app.AppError {

	// 校验代理名称
	if *name == "" {
		return app.NewAppError(app.ErrValidationFailed, "代理名称不能为空", nil)
	}

	// 校验路径
	if *locationPath == "" {
		*locationPath = "/"
	}
	if (*locationPath)[0] != '/' {
		return app.NewAppError(app.ErrValidationFailed, "代理路径必须以 / 开头", nil)
	}

	// 校验 upstream URL
	if *upstreamURL == "" {
		return app.NewAppError(app.ErrValidationFailed, "目标 URL 不能为空", nil)
	}
	if err := security.ValidateUpstreamURL(*upstreamURL); err != nil {
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
	if connectTimeout <= 0 {
		connectTimeout = 60
	}
	if connectTimeout > 3600 {
		return app.NewAppError(app.ErrValidationFailed, "connect_timeout 不能超过 3600 秒", nil)
	}
	if sendTimeout <= 0 {
		sendTimeout = 60
	}
	if sendTimeout > 3600 {
		return app.NewAppError(app.ErrValidationFailed, "send_timeout 不能超过 3600 秒", nil)
	}
	if readTimeout <= 0 {
		readTimeout = 60
	}
	if readTimeout > 3600 {
		return app.NewAppError(app.ErrValidationFailed, "read_timeout 不能超过 3600 秒", nil)
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
		ID:               p.ID,
		Name:             p.Name,
		Enabled:          p.Enabled,
		LocationPath:     p.LocationPath,
		UpstreamURL:      p.UpstreamURL,
		HostHeader:       p.HostHeader,
		WebSocketEnabled: p.WebSocketEnabled,
		ConnectTimeout:   p.ConnectTimeout,
		SendTimeout:      p.SendTimeout,
		ReadTimeout:      p.ReadTimeout,
		CacheEnabled:     p.CacheEnabled,
		CacheType:        p.CacheType,
		CacheTime:        p.CacheTime,
		AuthEnabled:      p.AuthEnabled,
		AuthAccountIDs:   accountIDs,
		AuthAccounts:     accountResponses,
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
