package geoaccess

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

type siteRepo interface {
	GetByID(id string) (*repo.Site, error)
}
type operationRepo interface {
	Create(*repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}
type agent interface {
	ApplyTransaction(context.Context, *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
	ReadFile(context.Context, string) ([]byte, string, error)
}

type capabilityAgent interface {
	DetectNginx(context.Context, *agentclient.NginxDetectRequest) (*agentclient.NginxDetectResponse, error)
}

type Service struct {
	repo       *repo.GeoAccessRepo
	sites      siteRepo
	ipRules    *repo.IPWhitelistRuleRepo
	operations operationRepo
	agent      agent
	dataDir    string
	panelDir   string
	key        []byte
	httpClient *http.Client
	mutationMu sync.Mutex
}

func NewService(repoStore *repo.GeoAccessRepo, sites siteRepo, ipRules *repo.IPWhitelistRuleRepo,
	operations operationRepo, agentClient agent, dataDir, panelDir string) (*Service, error) {
	key, err := loadOrCreateKey(dataDir)
	if err != nil {
		return nil, fmt.Errorf("初始化 GeoIP 密钥失败: %w", err)
	}
	return &Service{repo: repoStore, sites: sites, ipRules: ipRules, operations: operations,
		agent: agentClient, dataDir: dataDir, panelDir: panelDir, key: key, httpClient: newDownloadClient()}, nil
}

func (s *Service) GetSettings() (*GeoIPSettingsResponse, error) {
	item, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	secret, err := decryptSecret(s.key, item.LicenseKeyEncrypted)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, "GeoIP License Key 无法解密", nil)
	}
	return &GeoIPSettingsResponse{AccountID: item.AccountID, LicenseKeyMasked: maskSecret(secret),
		AutoUpdate: item.AutoUpdate, TrustedProxies: repo.ParseJSONStringSlice(item.TrustedProxiesJSON)}, nil
}

func normalizeTrustedProxies(values []string) ([]string, error) {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	if len(values) > 256 {
		return nil, fmt.Errorf("可信代理不能超过 256 条")
	}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(value); err == nil {
			value = prefix.Masked().String()
		} else if addr, err := netip.ParseAddr(value); err == nil {
			value = addr.String()
		} else {
			return nil, fmt.Errorf("无效可信代理 IP/CIDR: %s", value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func (s *Service) UpdateSettings(ctx context.Context, req UpdateGeoIPSettingsRequest, requestID string) (*GeoIPSettingsResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	old, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	updated := *old
	updated.AccountID = strings.TrimSpace(req.AccountID)
	if req.LicenseKey != nil && strings.TrimSpace(*req.LicenseKey) != "" {
		updated.LicenseKeyEncrypted, err = encryptSecret(s.key, strings.TrimSpace(*req.LicenseKey))
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
	}
	if req.AutoUpdate != nil {
		updated.AutoUpdate = *req.AutoUpdate
	}
	proxiesChanged := false
	if req.TrustedProxies != nil {
		proxies, normalizeErr := normalizeTrustedProxies(*req.TrustedProxies)
		if normalizeErr != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, normalizeErr.Error(), nil)
		}
		raw, _ := json.Marshal(proxies)
		proxiesChanged = string(raw) != old.TrustedProxiesJSON
		updated.TrustedProxiesJSON = string(raw)
	}
	if err := s.repo.SaveGeoIPSettings(&updated); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if proxiesChanged {
		if err := s.applyAll(ctx, "", requestID, "更新地域访问可信代理"); err != nil {
			_ = s.repo.SaveGeoIPSettings(old)
			return nil, err
		}
	}
	return s.GetSettings()
}

func (s *Service) GetStatus() (*GeoIPStatusResponse, error) {
	item, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	enabled, err := s.repo.ListEnabledSiteSettings()
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return &GeoIPStatusResponse{Installed: item.ActiveCachePath != "" && item.Checksum != "", Checksum: item.Checksum,
		BuildEpoch: item.BuildEpoch, Countries: repo.ParseJSONStringSlice(item.CountriesJSON),
		LastAttemptAt: item.LastAttemptAt, LastSuccessAt: item.LastSuccessAt, LastError: item.LastError,
		EnabledSites: len(enabled)}, nil
}

func (s *Service) InstallDatabase(ctx context.Context, reader io.Reader, requestID string) (*GeoIPStatusResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	tmp, err := tempMMDB(s.dataDir)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	written, copyErr := io.Copy(tmp, io.LimitReader(reader, maxMMDBBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		return nil, app.NewAppError(app.ErrBadRequest, "读取 GeoIP 数据库失败", nil)
	}
	if written > maxMMDBBytes {
		return nil, app.NewAppError(app.ErrValidationFailed, "GeoIP 数据库超过 64 MiB", nil)
	}
	if err := s.activateDatabase(ctx, tmpPath, requestID); err != nil {
		return nil, err
	}
	return s.GetStatus()
}

func (s *Service) UpdateDatabase(ctx context.Context, requestID string) (*GeoIPStatusResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	settings, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	settings.LastAttemptAt, settings.LastError = now, ""
	_ = s.repo.SaveGeoIPSettings(settings)
	license, err := decryptSecret(s.key, settings.LicenseKeyEncrypted)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, "GeoIP License Key 无法解密", nil)
	}
	tmp, err := tempMMDB(s.dataDir)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath)
	defer os.Remove(tmpPath)
	if err := downloadDatabase(s.httpClient, settings.AccountID, license, tmpPath); err != nil {
		settings.LastError = err.Error()
		_ = s.repo.SaveGeoIPSettings(settings)
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := s.activateDatabase(ctx, tmpPath, requestID); err != nil {
		return nil, err
	}
	return s.GetStatus()
}

func (s *Service) activateDatabase(ctx context.Context, sourcePath, requestID string) error {
	dbPath, cachePath, checksum, cache, err := installDatabaseFile(s.dataDir, sourcePath)
	if err != nil {
		return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	old, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	updated := *old
	updated.ActiveDBPath, updated.ActiveCachePath, updated.Checksum, updated.BuildEpoch = dbPath, cachePath, checksum, cache.BuildEpoch
	countries := make([]string, 0, len(cache.Networks)+1)
	for country := range cache.Networks {
		countries = append(countries, country)
	}
	sort.Strings(countries)
	if len(countries) == 0 || countries[len(countries)-1] != "ZZ" {
		countries = append(countries, "ZZ")
		sort.Strings(countries)
	}
	raw, _ := json.Marshal(countries)
	updated.CountriesJSON = string(raw)
	updated.LastAttemptAt = time.Now().UTC().Format(time.RFC3339)
	updated.LastSuccessAt = updated.LastAttemptAt
	updated.LastError = ""
	if err := s.repo.SaveGeoIPSettings(&updated); err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := s.applyAll(ctx, "", requestID, "更新 GeoLite2 数据库"); err != nil {
		_ = s.repo.SaveGeoIPSettings(old)
		return err
	}
	return nil
}

func (s *Service) GetSiteAccess(siteID string) (*SiteAccessResponse, error) {
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	item, err := s.repo.GetSiteSettings(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return siteSettingsResponse(item), nil
}

func (s *Service) UpdateSiteAccess(ctx context.Context, siteID string, req UpdateSiteAccessRequest, requestID string) (*SiteAccessResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	if !validAction(req.DefaultAction) {
		return nil, app.NewAppError(app.ErrValidationFailed, "默认动作无效", nil)
	}
	old, err := s.repo.GetSiteSettings(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	updated := *old
	updated.DefaultAction = req.DefaultAction
	if old.Enabled {
		updated.ApplyStatus = "pending"
	}
	if err := s.repo.SaveSiteSettings(&updated); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if old.Enabled {
		if err := s.applyAll(ctx, siteID, requestID, "更新站点地域默认动作"); err != nil {
			s.restoreSiteSettings(old, err)
			return nil, err
		}
	}
	return s.GetSiteAccess(siteID)
}

func (s *Service) EnableSite(ctx context.Context, siteID, requestID string) (*SiteAccessResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	old, err := s.repo.GetSiteSettings(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if old.Enabled {
		return siteSettingsResponse(old), nil
	}
	geo, err := s.repo.GetGeoIPSettings()
	if err != nil || geo.ActiveCachePath == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "请先安装 GeoLite2 Country 数据库", nil)
	}
	if detector, ok := s.agent.(capabilityAgent); ok {
		detected, detectErr := detector.DetectNginx(ctx, &agentclient.NginxDetectRequest{})
		if detectErr != nil {
			return nil, app.NewAppError(app.ErrAgentUnavailable, "检测 Nginx 地域访问能力失败: "+detectErr.Error(), nil)
		}
		if !detected.Capabilities["geo"] || !detected.Capabilities["map"] {
			return nil, app.NewAppError(app.ErrValidationFailed, "当前 Nginx 缺少 geo 或 map 模块", nil)
		}
		if len(repo.ParseJSONStringSlice(geo.TrustedProxiesJSON)) > 0 && !detected.Capabilities["realip"] {
			return nil, app.NewAppError(app.ErrValidationFailed, "配置可信代理需要 Nginx realip 模块", nil)
		}
	}
	updated := *old
	updated.Enabled = true
	updated.ApplyStatus = "pending"
	updated.LastError = ""
	if err := s.repo.SaveSiteSettings(&updated); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := s.applyAll(ctx, siteID, requestID, "启用站点地域访问"); err != nil {
		s.restoreSiteSettings(old, err)
		return nil, err
	}
	return s.GetSiteAccess(siteID)
}

func (s *Service) DisableSite(ctx context.Context, siteID, requestID string) (*SiteAccessResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	old, err := s.repo.GetSiteSettings(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if !old.Enabled {
		if old.ApplyStatus == "pending" {
			if err := s.applyAll(ctx, siteID, requestID, "继续未完成的站点地域访问关闭操作"); err != nil {
				return nil, err
			}
			return s.GetSiteAccess(siteID)
		}
		return siteSettingsResponse(old), nil
	}
	updated := *old
	updated.Enabled = false
	updated.ApplyStatus = "pending"
	updated.LastError = ""
	if err := s.repo.SaveSiteSettings(&updated); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := s.applyAll(ctx, siteID, requestID, "关闭站点地域访问"); err != nil {
		s.restoreSiteSettings(old, err)
		return nil, err
	}
	return s.GetSiteAccess(siteID)
}

func (s *Service) ListRules(siteID string) ([]*RuleResponse, error) {
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListRules(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	result := make([]*RuleResponse, 0, len(items))
	for _, item := range items {
		result = append(result, ruleResponse(item))
	}
	return result, nil
}

func (s *Service) CreateRule(ctx context.Context, siteID string, req CreateRuleRequest, requestID string) (*RuleResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	rules, err := s.repo.ListRules(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if len(rules) >= maxRules {
		return nil, app.NewAppError(app.ErrValidationFailed, "每个站点最多 64 条地域规则", nil)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 100 {
		return nil, app.NewAppError(app.ErrValidationFailed, "规则名称不能为空且不能超过 100 字符", nil)
	}
	if !validAction(req.Action) {
		return nil, app.NewAppError(app.ErrValidationFailed, "规则动作无效", nil)
	}
	countries, err := normalizeCountries(req.Countries)
	if err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	raw, _ := json.Marshal(countries)
	enabled := false
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	item := &repo.SiteGeoRule{ID: app.NewID("geo_rule"), SiteID: siteID, Name: name, CountriesJSON: string(raw), Action: req.Action, Enabled: enabled, SortOrder: len(rules)}
	if err := s.repo.CreateRule(item); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	settings, _ := s.repo.GetSiteSettings(siteID)
	if settings.Enabled && item.Enabled {
		if err := s.applyAll(ctx, siteID, requestID, "创建地域规则"); err != nil {
			_ = s.repo.DeleteRule(siteID, item.ID)
			s.markSiteError(settings, err)
			return nil, err
		}
	}
	created, _ := s.repo.GetRule(item.ID)
	return ruleResponse(created), nil
}

func (s *Service) UpdateRule(ctx context.Context, siteID, ruleID string, req UpdateRuleRequest, requestID string) (*RuleResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	old, err := s.repo.GetRule(ruleID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if old == nil || old.SiteID != siteID {
		return nil, app.NewAppError(app.ErrNotFound, "地域规则不存在", nil)
	}
	updated := *old
	if req.Name != nil {
		updated.Name = strings.TrimSpace(*req.Name)
		if updated.Name == "" || len(updated.Name) > 100 {
			return nil, app.NewAppError(app.ErrValidationFailed, "规则名称不能为空且不能超过 100 字符", nil)
		}
	}
	if req.Action != nil {
		if !validAction(*req.Action) {
			return nil, app.NewAppError(app.ErrValidationFailed, "规则动作无效", nil)
		}
		updated.Action = *req.Action
	}
	if req.Countries != nil {
		values, e := normalizeCountries(*req.Countries)
		if e != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, e.Error(), nil)
		}
		raw, _ := json.Marshal(values)
		updated.CountriesJSON = string(raw)
	}
	if req.Enabled != nil {
		updated.Enabled = *req.Enabled
	}
	if err := s.repo.UpdateRule(&updated); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	settings, _ := s.repo.GetSiteSettings(siteID)
	if settings.Enabled && (old.Enabled || updated.Enabled) {
		if err := s.applyAll(ctx, siteID, requestID, "更新地域规则"); err != nil {
			_ = s.repo.UpdateRule(old)
			s.markSiteError(settings, err)
			return nil, err
		}
	}
	item, _ := s.repo.GetRule(ruleID)
	return ruleResponse(item), nil
}

func (s *Service) DeleteRule(ctx context.Context, siteID, ruleID, requestID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := s.requireSite(siteID); err != nil {
		return err
	}
	old, err := s.repo.GetRule(ruleID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if old == nil || old.SiteID != siteID {
		return app.NewAppError(app.ErrNotFound, "地域规则不存在", nil)
	}
	if err := s.repo.DeleteRule(siteID, ruleID); err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	settings, _ := s.repo.GetSiteSettings(siteID)
	if settings.Enabled && old.Enabled {
		if err := s.applyAll(ctx, siteID, requestID, "删除地域规则"); err != nil {
			_ = s.repo.CreateRule(old)
			s.markSiteError(settings, err)
			return err
		}
	}
	return nil
}

func (s *Service) ReorderRules(ctx context.Context, siteID string, ids []string, requestID string) ([]*RuleResponse, error) {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	old, err := s.repo.ListRules(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	oldIDs := make([]string, len(old))
	for i, item := range old {
		oldIDs[i] = item.ID
	}
	if err := s.repo.ReorderRules(siteID, ids); err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	settings, _ := s.repo.GetSiteSettings(siteID)
	if settings.Enabled {
		if err := s.applyAll(ctx, siteID, requestID, "调整地域规则顺序"); err != nil {
			_ = s.repo.ReorderRules(siteID, oldIDs)
			s.markSiteError(settings, err)
			return nil, err
		}
	}
	return s.ListRules(siteID)
}

func (s *Service) IsSiteEnabled(siteID string) bool {
	item, err := s.repo.GetSiteSettings(siteID)
	return err == nil && item.Enabled
}

func (s *Service) SyncAfterIPChange(ctx context.Context, siteID, requestID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	if !s.IsSiteEnabled(siteID) {
		return nil
	}
	return s.applyAll(ctx, siteID, requestID, "同步地域访问 IP 例外")
}

func (s *Service) Reconcile(ctx context.Context) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	pendingDisabled, err := s.repo.ListPendingDisabledSiteIDs()
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	for _, siteID := range pendingDisabled {
		if err := s.applyAll(ctx, siteID, "startup", "恢复未完成的地域访问关闭操作"); err != nil {
			if item, getErr := s.repo.GetSiteSettings(siteID); getErr == nil {
				item.LastError = err.Error()
				_ = s.repo.SaveSiteSettings(item)
			}
			return err
		}
	}
	if len(pendingDisabled) > 0 {
		return nil
	}
	err = s.applyAll(ctx, "", "startup", "对账地域访问配置")
	if err == nil {
		return nil
	}
	settings, listErr := s.repo.ListEnabledSiteSettings()
	if listErr == nil {
		for _, setting := range settings {
			s.markSiteError(setting, err)
		}
	}
	return err
}

func (s *Service) requireSite(siteID string) (*repo.Site, error) {
	site, err := s.sites.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	return site, nil
}

func (s *Service) restoreSiteSettings(old *repo.SiteGeoSettings, applyErr error) {
	restored := *old
	restored.LastError = applyErr.Error()
	restored.ApplyStatus = "error"
	_ = s.repo.SaveSiteSettings(&restored)
}

func (s *Service) markSiteError(settings *repo.SiteGeoSettings, applyErr error) {
	if settings == nil {
		return
	}
	item := *settings
	item.LastError = applyErr.Error()
	item.ApplyStatus = "error"
	_ = s.repo.SaveSiteSettings(&item)
}

func (s *Service) applyAll(ctx context.Context, affectedSiteID, requestID, message string) error {
	settings, err := s.repo.ListEnabledSiteSettings()
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if len(settings) == 0 && affectedSiteID == "" {
		return nil
	}
	geoSettings, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	cache := &countryCache{Networks: map[string][]string{}}
	if len(settings) > 0 {
		if geoSettings.ActiveCachePath == "" {
			return app.NewAppError(app.ErrValidationFailed, "GeoLite2 Country 数据库未安装", nil)
		}
		cache, err = readCache(geoSettings.ActiveCachePath)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, "读取 GeoIP 国家缓存失败: "+err.Error(), nil)
		}
	}
	rulesBySite := make(map[string][]*repo.SiteGeoRule, len(settings))
	ipBySite := make(map[string][]*repo.SiteIPWhitelistRule, len(settings))
	for _, setting := range settings {
		rulesBySite[setting.SiteID], err = s.repo.ListRules(setting.SiteID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		ipBySite[setting.SiteID], err = s.ipRules.ListBySiteID(setting.SiteID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
	}
	trusted, err := normalizeTrustedProxies(repo.ParseJSONStringSlice(geoSettings.TrustedProxiesJSON))
	if err != nil {
		return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	rendered, err := renderConfiguration(s.panelDir, trusted, cache, settings, rulesBySite, ipBySite)
	if err != nil {
		return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	changes := make([]agentclient.FileChangeRequest, 0, len(rendered.Sites)*3+5)
	if len(settings) == 0 {
		changes = append(changes, agentclient.FileChangeRequest{Type: "remove", Path: rendered.GlobalPath})
	} else {
		changes = append(changes, fileWrite(rendered.GlobalPath, rendered.GlobalContent, 0644))
	}
	reload := false
	for _, setting := range settings {
		siteRender := rendered.Sites[setting.SiteID]
		changes = append(changes, fileWrite(siteRender.SitePath, siteRender.SiteContent, 0644),
			fileWrite(siteRender.AllowPath, siteRender.AllowBody, 0644), fileWrite(siteRender.DenyPath, siteRender.DenyBody, 0644))
		site, _ := s.sites.GetByID(setting.SiteID)
		if site != nil && site.Status == "enabled" {
			reload = true
		}
	}
	patchTargets := make([]string, 0, len(settings)+1)
	if affectedSiteID != "" {
		patchTargets = append(patchTargets, affectedSiteID)
	} else {
		for _, setting := range settings {
			patchTargets = append(patchTargets, setting.SiteID)
		}
	}
	for _, siteID := range patchTargets {
		site, siteErr := s.requireSite(siteID)
		if siteErr != nil {
			return siteErr
		}
		if site.AccessLimitPath == "" {
			site.AccessLimitPath = filepath.Join(s.panelDir, "access-limit", site.PrimaryDomain+".conf")
		}
		current, _, readErr := s.agent.ReadFile(ctx, site.AccessLimitPath)
		if readErr != nil {
			return app.NewAppError(app.ErrAgentUnavailable, "读取站点访问限制配置失败: "+readErr.Error(), nil)
		}
		siteRender, enabled := rendered.Sites[siteID]
		includePath := ""
		if enabled {
			includePath = siteRender.SitePath
		}
		changes = append(changes, fileWrite(site.AccessLimitPath, string(patchAccessLimit(current, includePath, enabled)), 0644))
		if enabled {
			if strings.TrimSpace(site.ConfigPath) == "" {
				return app.NewAppError(app.ErrValidationFailed, "站点主配置路径为空，无法注入访问限制入口", nil)
			}
			mainConfig, _, configReadErr := s.agent.ReadFile(ctx, site.ConfigPath)
			if configReadErr != nil {
				return app.NewAppError(app.ErrAgentUnavailable, "读取站点主配置失败: "+configReadErr.Error(), nil)
			}
			if len(mainConfig) == 0 {
				return app.NewAppError(app.ErrValidationFailed, "站点主配置为空，无法注入访问限制入口", nil)
			}
			markerBody := []byte("    include " + site.AccessLimitPath + ";\n")
			patchedMainConfig, markerErr := nginx.EnsureMarkerBlock(mainConfig, nginx.MarkerNameAccessLimit, markerBody)
			if markerErr != nil {
				return app.NewAppError(app.ErrValidationFailed, "注入站点访问限制入口失败: "+markerErr.Error(), nil)
			}
			changes = append(changes, fileWrite(site.ConfigPath, string(patchedMainConfig), 0644))
		}
		if !enabled {
			changes = append(changes,
				agentclient.FileChangeRequest{Type: "remove", Path: filepath.Join(s.panelDir, "geo-access", "sites", siteID+".conf")},
				agentclient.FileChangeRequest{Type: "remove", Path: filepath.Join(s.panelDir, "geo-access", "ip", siteID+"-allow.conf")},
				agentclient.FileChangeRequest{Type: "remove", Path: filepath.Join(s.panelDir, "geo-access", "ip", siteID+"-deny.conf")})
		}
		if site.Status == "enabled" {
			reload = true
		}
	}
	if len(changes) == 0 {
		return nil
	}
	opID := app.NewOperationID()
	_ = s.operations.Create(&repo.Operation{ID: opID, Action: "site.update_geo_access", TargetType: "site",
		TargetID: affectedSiteID, Status: "pending", RequestID: requestID, Actor: "admin", Message: message,
		CreatedAt: time.Now().UTC().Format(time.RFC3339)})
	_, err = s.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{OperationID: opID, Changes: changes,
		TestNginx: true, ReloadNginx: reload, TimeoutSeconds: 60})
	if err != nil {
		_ = s.operations.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "应用地域访问配置失败: "+err.Error(), nil)
	}
	_ = s.operations.UpdateStatus(opID, "success")
	for _, setting := range settings {
		if siteRender := rendered.Sites[setting.SiteID]; siteRender != nil {
			setting.DesiredHash, setting.AppliedHash, setting.ApplyStatus, setting.LastError = siteRender.Hash, siteRender.Hash, "applied", ""
			_ = s.repo.SaveSiteSettings(setting)
		}
	}
	if affectedSiteID != "" {
		item, _ := s.repo.GetSiteSettings(affectedSiteID)
		if item != nil && !item.Enabled {
			item.DesiredHash, item.AppliedHash, item.ApplyStatus, item.LastError = "", "", "disabled", ""
			_ = s.repo.SaveSiteSettings(item)
		}
	}
	return nil
}

func fileWrite(path, content string, perm uint32) agentclient.FileChangeRequest {
	return agentclient.FileChangeRequest{Type: "write", Path: path, ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)), Perm: perm}
}
