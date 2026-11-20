package geoaccess

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/georuntime"
	"github.com/luoye663/nxpanel/internal/nginx"
)

const moduleMarker = "# NXPANEL-GEOIP2-MODULE\n"

type RuntimeStatus struct {
	State            string `json:"state"`
	Module           string `json:"module"`
	MigratedIncludes int    `json:"migrated_includes"`
	LegacyIncludes   int    `json:"legacy_includes"`
	Message          string `json:"message,omitempty"`
	OperationID      string `json:"operation_id,omitempty"`
}

type runtimeAgent interface {
	FilesList(context.Context, string) (*agentclient.FilesListResponse, error)
	FilesDownload(context.Context, string) (*http.Response, error)
	NginxStatus(context.Context) (*nginx.RuntimeStatus, error)
}

func (s *Service) ConfigureRuntime(mainConfig, modulePath string, prepare func(context.Context, map[string]georuntime.Snapshot, func() (map[string][]string, error)) ([]agentclient.FileChangeRequest, int, int, error)) {
	s.mainConfig, s.modulePath, s.policyMigrate = mainConfig, modulePath, prepare
}

func (s *Service) runtimeFiles(ctx context.Context, inspectLegacy bool) (map[string]georuntime.Snapshot, error) {
	client, ok := s.agent.(runtimeAgent)
	if !ok {
		return nil, fmt.Errorf("Agent 不支持 GeoIP2 配置检查")
	}
	directory := filepath.Join(s.panelDir, "conf.d")
	listing, err := client.FilesList(ctx, directory)
	if err != nil {
		return nil, err
	}
	files := make(map[string]georuntime.Snapshot)
	for _, entry := range listing.Entries {
		if filepath.Base(entry.Name) != entry.Name || !strings.HasSuffix(entry.Name, ".conf") {
			continue
		}
		path := filepath.Join(directory, entry.Name)
		if path != georuntime.ConfigPath(s.panelDir) && (!inspectLegacy || entry.Name != georuntime.LegacyInclude && !strings.HasPrefix(entry.Name, georuntime.PolicyIncludePrefix)) {
			continue
		}
		if entry.IsDir {
			return nil, fmt.Errorf("预期地域配置为普通文件: %s", path)
		}
		response, err := client.FilesDownload(ctx, path)
		if err != nil {
			return nil, err
		}
		snapshot, err := georuntime.ReadSnapshot(response.Body)
		_ = response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("检查 %s: %w", path, err)
		}
		files[path] = snapshot
	}
	return files, nil
}

func (s *Service) EnsureRuntime(ctx context.Context, requestID string) error {
	if s.policyMigrate == nil {
		return nil
	}
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	if s.runtimeState.State == "ready" || s.runtimeState.State == "partial" {
		return nil
	}
	settings, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return err
	}
	if settings.ActiveDBPath == "" {
		return app.ErrValidationFailedMsg("请先安装 GeoLite2 Country 数据库", nil)
	}
	files, err := s.runtimeFiles(ctx, false)
	if err == nil {
		err = s.applyRuntime(ctx, files, settings.ActiveDBPath, nil, false, requestID)
	}
	if err != nil {
		if s.runtimeState.State != "pending" {
			s.runtimeState.State, s.runtimeState.Message = "blocked", err.Error()
		}
		return app.ErrValidationFailedMsg("GeoIP2 尚不可用，请检查 MMDB 和当前 Nginx/OpenResty 的兼容模块："+err.Error(), nil)
	}
	if s.runtimeState.LegacyIncludes > 0 {
		s.runtimeState.State, s.runtimeState.Message = "partial", "共享查询已就绪，但仍有旧地域配置在运行"
	}
	return nil
}

func (s *Service) Migrate(ctx context.Context, requestID string) error {
	s.mutationMu.Lock()
	defer s.mutationMu.Unlock()
	s.runtimeMu.Lock()
	defer s.runtimeMu.Unlock()
	settings, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return err
	}
	return s.migrateRuntime(ctx, settings, requestID)
}

func (s *Service) switchRuntimeDatabase(ctx context.Context, current *repo.GeoIPSettings, candidate, requestID string) error {
	if s.policyMigrate == nil {
		return nil
	}
	files, err := s.runtimeFiles(ctx, true)
	if err != nil {
		return err
	}
	legacyPath := filepath.Join(s.panelDir, "conf.d", georuntime.LegacyInclude)
	for path, snapshot := range files {
		if !snapshot.Legacy && (path != legacyPath || snapshot.Shared) {
			continue
		}
		if err := s.migrateRuntime(ctx, current, requestID); err != nil {
			return app.ErrValidationFailedMsg("须先完成旧地域配置迁移才能切换 MMDB："+err.Error(), nil)
		}
		files, err = s.runtimeFiles(ctx, true)
		if err != nil {
			return err
		}
		break
	}
	_, active := files[georuntime.ConfigPath(s.panelDir)]
	for _, snapshot := range files {
		active = active || snapshot.Shared
	}
	if active && (current.ActiveDBPath != candidate || s.runtimeState.State != "ready") {
		if err := s.applyRuntime(ctx, files, candidate, nil, false, requestID); err != nil {
			return app.ErrAgentUnavailableMsg("GeoIP2 数据库切换未确认：" + err.Error())
		}
	}
	return nil
}

func (s *Service) migrateRuntime(ctx context.Context, settings *repo.GeoIPSettings, requestID string) (result error) {
	if s.policyMigrate == nil {
		return nil
	}
	defer func() {
		if result != nil && s.runtimeState.State != "pending" && s.runtimeState.State != "partial" {
			s.runtimeState.State, s.runtimeState.Message = "blocked", result.Error()
		}
	}()
	files, err := s.runtimeFiles(ctx, true)
	if err != nil {
		return err
	}
	s.runtimeState.LegacyIncludes = 0
	for _, snapshot := range files {
		if snapshot.Legacy {
			s.runtimeState.LegacyIncludes++
		}
	}
	var oldCache *countryCache
	networks := func() (map[string][]string, error) {
		if oldCache == nil {
			oldCache, err = readCache(settings.ActiveCachePath)
			if err != nil {
				return nil, fmt.Errorf("无法核验旧地域配置：%w", err)
			}
		}
		return oldCache.Networks, nil
	}
	changes, migrated, blocked, err := s.policyMigrate(ctx, files, networks)
	if err != nil {
		return err
	}
	legacyPath := filepath.Join(s.panelDir, "conf.d", georuntime.LegacyInclude)
	if previous, exists := files[legacyPath]; exists {
		if previous.Shared && !previous.Legacy {
			migrated++
		} else {
			change, err := s.prepareLegacyMigration(previous, settings, networks)
			if err != nil {
				return err
			}
			if change == nil {
				blocked++
			} else {
				changes = append(changes, *change)
				migrated++
			}
		}
	}
	_, hasShared := files[georuntime.ConfigPath(s.panelDir)]
	s.runtimeState.LegacyIncludes = blocked + len(changes)
	s.runtimeState.MigratedIncludes = migrated - len(changes)
	if len(changes) == 0 && migrated == 0 && (!hasShared || blocked == 0) {
		if blocked > 0 {
			return fmt.Errorf("有 %d 份旧地域配置无法安全核验；保留原配置，请检查手工修改、已删除站点或待同步策略", blocked)
		}
		if hasShared {
			if err := s.retireRuntime(ctx, files, requestID); err != nil {
				return err
			}
		}
		s.runtimeState = RuntimeStatus{State: "idle", Module: "unchecked"}
		return nil
	}
	if len(changes) != 0 || s.runtimeState.State != "ready" && s.runtimeState.State != "partial" {
		if err := s.applyRuntime(ctx, files, settings.ActiveDBPath, changes, len(changes) > 0, requestID); err != nil {
			return err
		}
	}
	s.runtimeState.MigratedIncludes, s.runtimeState.LegacyIncludes = migrated, blocked
	if blocked > 0 {
		message := fmt.Sprintf("有 %d 份旧地域配置未经修改；请检查手工修改、已删除站点或待同步策略", blocked)
		s.runtimeState.State, s.runtimeState.Message = "partial", message
		return fmt.Errorf("%s", message)
	}
	s.runtimeState.State, s.runtimeState.Message = "ready", ""
	return nil
}

func (s *Service) prepareLegacyMigration(previous georuntime.Snapshot, settings *repo.GeoIPSettings, networks func() (map[string][]string, error)) (*agentclient.FileChangeRequest, error) {
	legacy, rules, ipRules, err := s.legacyRenderData()
	if err != nil || len(legacy) == 0 {
		return nil, err
	}
	for _, setting := range legacy {
		if setting.ApplyStatus != "applied" || setting.AppliedHash == "" || setting.DesiredHash != setting.AppliedHash {
			return nil, nil
		}
	}
	entries, err := networks()
	if err != nil {
		return nil, err
	}
	trusted, err := normalizeTrustedProxies(repo.ParseJSONStringSlice(settings.TrustedProxiesJSON))
	if err != nil {
		return nil, err
	}
	digest := sha256.New()
	old, err := renderConfig(s.panelDir, trusted, settings.BuildEpoch, legacy, rules, ipRules, &countryCache{Networks: entries}, digest)
	if err != nil || fmt.Sprintf("%x", digest.Sum(nil)) != previous.Hash {
		return nil, err
	}
	for _, setting := range legacy {
		if generated := old.Sites[setting.SiteID]; generated == nil || generated.Hash != setting.AppliedHash {
			return nil, nil
		}
	}
	next, err := renderConfiguration(s.panelDir, trusted, settings.BuildEpoch, legacy, rules, ipRules)
	if err != nil {
		return nil, err
	}
	change := fileWrite(next.GlobalPath, next.GlobalContent, 0644)
	change.ExpectedHash = previous.Hash
	return &change, nil
}

func (s *Service) legacyRenderData() ([]*repo.SiteGeoSettings, map[string][]*repo.SiteGeoRule, map[string][]*repo.SiteIPWhitelistRule, error) {
	settings, err := s.repo.ListEnabledSiteSettings()
	if err != nil {
		return nil, nil, nil, err
	}
	legacy := make([]*repo.SiteGeoSettings, 0, len(settings))
	rules := make(map[string][]*repo.SiteGeoRule)
	ipRules := make(map[string][]*repo.SiteIPWhitelistRule)
	for _, setting := range settings {
		if s.policyManaged != nil && s.policyManaged(setting.SiteID) {
			continue
		}
		legacy = append(legacy, setting)
		rules[setting.SiteID], err = s.repo.ListRules(setting.SiteID)
		if err != nil {
			return nil, nil, nil, err
		}
		ipRules[setting.SiteID], err = s.ipRules.ListBySiteID(setting.SiteID)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return legacy, rules, ipRules, nil
}

func (s *Service) applyRuntime(ctx context.Context, files map[string]georuntime.Snapshot, databasePath string, changes []agentclient.FileChangeRequest, migration bool, requestID string) error {
	config, err := georuntime.Config(databasePath)
	if err != nil {
		return err
	}
	path := georuntime.ConfigPath(s.panelDir)
	current, exists := files[path]
	if exists && !strings.HasPrefix(string(current.Prefix), georuntime.SharedHeader) {
		return fmt.Errorf("共享地域配置已被手工管理，未修改: %s", path)
	}
	shared := fileWrite(path, config, 0644)
	shared.ExpectedHash = current.Hash
	changes = append(changes, shared)
	if s.modulePath != "" {
		module, err := georuntime.QuotePath(s.modulePath)
		if err != nil {
			return err
		}
		main, hash, err := s.agent.ReadFile(ctx, s.mainConfig)
		if err != nil {
			return err
		}
		directive := "load_module " + module + ";\n"
		if !strings.Contains(string(main), "\n"+directive) && !strings.HasPrefix(string(main), directive) {
			if strings.Contains(string(main), moduleMarker) {
				return fmt.Errorf("现有 GeoIP2 模块标记已修改，未覆盖 Nginx 主配置")
			}
			change := fileWrite(s.mainConfig, moduleMarker+directive+string(main), 0644)
			change.ExpectedHash, change.FollowSymlink = hash, true
			changes = append(changes, change)
		}
	}
	return s.commitRuntime(ctx, changes, migration, requestID)
}

func (s *Service) retireRuntime(ctx context.Context, files map[string]georuntime.Snapshot, requestID string) error {
	path := georuntime.ConfigPath(s.panelDir)
	current := files[path]
	if !strings.HasPrefix(string(current.Prefix), georuntime.SharedHeader) {
		return fmt.Errorf("共享地域配置已被手工管理，未修改: %s", path)
	}
	changes := []agentclient.FileChangeRequest{
		{Type: "check", Path: path, ExpectedHash: current.Hash},
		{Type: "remove", Path: path},
	}
	if s.modulePath != "" {
		module, err := georuntime.QuotePath(s.modulePath)
		if err != nil {
			return err
		}
		main, hash, err := s.agent.ReadFile(ctx, s.mainConfig)
		if err != nil {
			return err
		}
		remaining, present := strings.CutPrefix(string(main), moduleMarker+"load_module "+module+";\n")
		if present {
			change := fileWrite(s.mainConfig, remaining, 0644)
			change.ExpectedHash, change.FollowSymlink = hash, true
			changes = append(changes, change)
		}
	}
	return s.commitRuntime(ctx, changes, false, requestID)
}

func (s *Service) commitRuntime(ctx context.Context, changes []agentclient.FileChangeRequest, migration bool, requestID string) error {
	status, err := s.agent.(runtimeAgent).NginxStatus(ctx)
	if err != nil {
		return err
	}
	opID := app.NewOperationID()
	if err := s.operations.Create(&repo.Operation{ID: opID, Action: "geoip.apply_runtime", TargetType: "geoip", Status: "pending", RequestID: requestID, Actor: "admin", Message: "切换共享 GeoIP2 查询配置", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	s.runtimeState.OperationID = opID
	response, err := s.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{OperationID: opID, Changes: changes, TestNginx: true,
		ReloadNginx: status.Running, ConfirmReload: status.Running, PreserveBackup: migration, TimeoutSeconds: 60})
	if err == nil && response == nil {
		err = fmt.Errorf("Agent 未返回文件事务确认")
	}
	if err != nil {
		_ = s.operations.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		s.runtimeState.State, s.runtimeState.Message = "pending", "未确认 GeoIP2 生效；保留已确认数据库版本，可重试："+err.Error()
		if agentclient.IsTransactionRejected(err) {
			s.runtimeState.State, s.runtimeState.Message = "blocked", err.Error()
			if strings.Contains(err.Error(), `unknown directive "geoip2"`) || strings.Contains(err.Error(), "ngx_http_geoip2_module.so") {
				s.runtimeState.Module = "unavailable"
			}
		}
		return err
	}
	_ = s.operations.UpdateStatus(opID, "success")
	s.runtimeState.State, s.runtimeState.Message = "ready", ""
	s.runtimeState.Module = "available"
	return nil
}
