package accesspolicy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
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

type policyAgent interface {
	ReadFile(context.Context, string) ([]byte, string, error)
	ApplyTransaction(context.Context, *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
}

type countryProvider interface {
	AccessPolicyData() (map[string][]string, []string, error)
}

// ProxyCoordinator holds the proxy writer lock while preparing and applying a
// policy. Only the proxy service patches its own MAIN/EXTRA-LOCATION markers.
type ProxyCoordinator interface {
	WithAccessPolicyConfig(context.Context, string, func([]byte) error) error
}

type Service struct {
	store           *Repo
	sites           *repo.SiteRepo
	accounts        *repo.AuthAccountRepo
	operations      *repo.OperationRepo
	agent           policyAgent
	geo             countryProvider
	proxy           ProxyCoordinator
	panelDir        string
	nginxConfigPath string
	mu              sync.Mutex
	leasesMu        sync.Mutex
	leases          map[string]*siteLease
}

func NewService(db *sql.DB, agent policyAgent, geo countryProvider, proxy ProxyCoordinator, panelDir string) *Service {
	return &Service{store: NewRepo(db), sites: repo.NewSiteRepo(db), accounts: repo.NewAuthAccountRepo(db), operations: repo.NewOperationRepo(db), agent: agent, geo: geo, proxy: proxy, panelDir: panelDir}
}

func (s *Service) Managed(siteID string) bool {
	mode, err := s.store.Mode(siteID)
	return err == nil && mode == "unified"
}

// GuardLegacy also blocks writes during a failed/uncertain first activation.
// Otherwise a stale editor could replace files that the agent already applied.
func (s *Service) GuardLegacy(siteID string) error {
	p, err := s.store.Get(siteID)
	if err != nil {
		return err
	}
	if p != nil {
		return app.ErrConflictMsg("该站点已进入统一访问规则编排，请刷新页面并在访问限制中编辑或同步策略")
	}
	return nil
}

func (s *Service) LegacyLease(siteID string) (func(), error) {
	release := s.acquireSite(siteID)
	if err := s.GuardLegacy(siteID); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (s *Service) requireSite(siteID string) (*repo.Site, error) {
	site, err := s.sites.GetByID(siteID)
	if err != nil {
		return nil, err
	}
	if site == nil {
		return nil, app.ErrNotFoundMsg("站点不存在")
	}
	if site.ConfigPath == "" {
		return nil, app.ErrValidationFailedMsg("站点没有可管理的配置文件", nil)
	}
	return site, nil
}

func (s *Service) Get(siteID string) (*Policy, error) {
	if _, err := s.requireSite(siteID); err != nil {
		return nil, err
	}
	p, err := s.store.Get(siteID)
	if err != nil || p != nil {
		return p, err
	}
	return s.store.LegacyDraft(siteID)
}

func (s *Service) renderContext(site *repo.Site, p Policy) (RenderContext, []string, error) {
	if err := s.validateAccountReferences(site.ID, p); err != nil {
		return RenderContext{}, nil, err
	}
	renderCtx := RenderContext{AuthFiles: map[string]string{}}
	renderCtx.ServerNames = append(repo.ParseJSONStringSlice(site.DomainsJSON), site.PrimaryDomain)
	needsCountry := false
	for _, rule := range p.Rules {
		if !rule.Enabled || rule.SourceDisabled {
			continue
		}
		for _, c := range rule.Conditions {
			if c.Kind == "country" {
				needsCountry = true
			}
		}
	}
	var proxies []string
	if s.geo != nil {
		var err error
		renderCtx.CountryNetworks, proxies, err = s.geo.AccessPolicyData()
		if err != nil {
			return renderCtx, nil, err
		}
	}
	if needsCountry && len(renderCtx.CountryNetworks) == 0 {
		return renderCtx, nil, app.ErrValidationFailedMsg("请先安装 GeoLite2 Country 数据库再启用地域条件", nil)
	}
	for _, rule := range p.Rules {
		if !rule.Enabled || rule.SourceDisabled || rule.Action.Type != "auth" {
			continue
		}
		body, err := s.authBody(site.ID, rule.Action.AccountIDs)
		if err != nil {
			return renderCtx, nil, fmt.Errorf("规则 %s: %w", rule.Name, err)
		}
		renderCtx.AuthFiles[rule.ID] = body
	}
	if p.DefaultAction.Type == "auth" {
		body, err := s.authBody(site.ID, p.DefaultAction.AccountIDs)
		if err != nil {
			return renderCtx, nil, err
		}
		renderCtx.AuthFiles["default"] = body
	}
	return renderCtx, proxies, nil
}

func (s *Service) authBody(siteID string, ids []string) (string, error) {
	accounts, err := s.accounts.ListByIDs(ids)
	if err != nil {
		return "", err
	}
	if len(accounts) != len(ids) {
		return "", app.ErrValidationFailedMsg("引用的访问账户不存在", nil)
	}
	var entries []string
	seen := map[string]bool{}
	for _, account := range accounts {
		if account.Scope != "global" && account.SiteID != siteID {
			return "", app.ErrValidationFailedMsg("访问账户不属于当前站点", nil)
		}
		if !account.Enabled {
			continue
		}
		if seen[account.Username] {
			return "", app.ErrValidationFailedMsg("同一密码规则不能引用重名账户", nil)
		}
		seen[account.Username] = true
		if strings.ContainsAny(account.PasswordHash, "\r\n\x00") || !strings.HasPrefix(account.PasswordHash, account.Username+":") {
			return "", app.ErrValidationFailedMsg("访问账户密码记录无效", nil)
		}
		entries = append(entries, account.PasswordHash)
	}
	if len(entries) == 0 {
		return "", app.ErrValidationFailedMsg("密码规则至少需要一个启用的访问账户", nil)
	}
	sort.Strings(entries)
	return strings.Join(entries, "\n") + "\n", nil
}

func (s *Service) Preview(siteID string, p Policy, request PreviewRequest) (*PreviewResult, error) {
	site, err := s.requireSite(siteID)
	if err != nil {
		return nil, err
	}
	if err := s.resolveSources(siteID, &p); err != nil {
		return nil, err
	}
	ctx, _, err := s.renderContext(site, p)
	if err != nil {
		return nil, err
	}
	// Country is calculated from the submitted client IP; it is not a second,
	// independently spoofable preview input.
	request.Country = ""
	return Preview(p, request, ctx)
}

func (s *Service) Save(ctx context.Context, siteID string, p Policy, requestID string) (*Policy, error) {
	site, err := s.requireSite(siteID)
	if err != nil {
		return nil, err
	}
	p.PendingSource = ""
	if err := s.resolveSources(siteID, &p); err != nil {
		return nil, err
	}
	if err := Validate(p); err != nil {
		return nil, app.ErrValidationFailedMsg(err.Error(), nil)
	}
	renderCtx, proxies, err := s.renderContext(site, p)
	if err != nil {
		return nil, err
	}
	compiled, err := Compile(site.ID, s.panelDir, p, renderCtx)
	if err != nil {
		return nil, app.ErrValidationFailedMsg(err.Error(), nil)
	}
	if err := s.apply(ctx, site, p, compiled, proxies, renderCtx.AuthFiles, requestID, true, true); err != nil {
		return nil, err
	}
	return s.store.Get(siteID)
}

func (s *Service) Sync(ctx context.Context, siteID, requestID string) (*Policy, error) {
	p, err := s.store.Get(siteID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, app.ErrConflictMsg("请先保存统一访问策略")
	}
	if p.PendingSource == "proxy" && p.ApplyStatus != "applied" {
		syncer, ok := s.proxy.(interface {
			Sync(context.Context, string, string) (string, error)
		})
		if !ok {
			return nil, app.ErrConflictMsg("请在反向代理页面同步未完成的关联修改")
		}
		if _, err := syncer.Sync(ctx, siteID, requestID); err != nil {
			return nil, err
		}
		return s.store.Get(siteID)
	}
	if err := s.applyPolicy(ctx, siteID, *p, requestID, true); err != nil {
		return nil, err
	}
	return s.store.Get(siteID)
}

func (s *Service) applyPolicy(ctx context.Context, siteID string, p Policy, requestID string, mark bool) error {
	site, err := s.requireSite(siteID)
	if err != nil {
		return err
	}
	renderCtx, proxies, err := s.renderContext(site, p)
	if err != nil {
		return err
	}
	compiled, err := Compile(site.ID, s.panelDir, p, renderCtx)
	if err != nil {
		return err
	}
	return s.apply(ctx, site, p, compiled, proxies, renderCtx.AuthFiles, requestID, mark, false)
}

func policyFile(path, content string, perm uint32) agentclient.FileChangeRequest {
	return agentclient.FileChangeRequest{Type: "write", Path: path, ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)), Perm: perm}
}

func (s *Service) apply(ctx context.Context, site *repo.Site, p Policy, compiled *CompiledPolicy, proxies []string, authFiles map[string]string, requestID string, mark, save bool) error {
	release := s.acquireSite(site.ID)
	defer release()
	opID := app.NewOperationID()
	if err := s.operations.Create(&repo.Operation{ID: opID, Action: "site.update_access_policy", TargetType: "site", TargetID: site.ID, Status: "pending", RequestID: requestID, Actor: "admin", Message: "应用统一访问规则编排", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	desiredSaved := false
	apply := func(main []byte) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !mark {
			applied, readErr := s.store.GetApplied(site.ID)
			if readErr != nil {
				return readErr
			}
			if applied == nil || applied.Version != p.Version {
				return nil
			}
		}
		if mark {
			if err := s.resolveSources(site.ID, &p); err != nil {
				return err
			}
		}
		renderCtx, currentProxies, prepErr := s.renderContext(site, p)
		if prepErr != nil {
			return app.ErrValidationFailedMsg(prepErr.Error(), nil)
		}
		currentCompiled, prepErr := Compile(site.ID, s.panelDir, p, renderCtx)
		if prepErr != nil {
			return app.ErrValidationFailedMsg(prepErr.Error(), nil)
		}
		compiled, proxies, authFiles = currentCompiled, currentProxies, renderCtx.AuthFiles
		// Custom access directives outside our owned markers cannot be made
		// reorderable safely. Never silently remove or override them.
		if err := s.CheckConfig(ctx, site, main); err != nil {
			return err
		}
		accessPath := site.AccessLimitPath
		if accessPath == "" {
			accessPath = filepath.Join(s.panelDir, "access-limit", site.PrimaryDomain+".conf")
		}
		patched, err := nginx.ApplyOptionalMarkerPatches(main, []nginx.BlockPatch{
			{Name: nginx.MarkerNameAccessLimit, Body: []byte("    include " + accessPath + ";\n")},
			{Name: nginx.MarkerNameHotlink, Body: nil},
		})
		if err != nil {
			return err
		}
		if save {
			current, readErr := s.store.Get(site.ID)
			if readErr != nil {
				return readErr
			}
			if current != nil && current.PendingSource == "proxy" && current.ApplyStatus != "applied" {
				return app.ErrConflictMsg("反代与访问规则的关联修改尚未完成，请先同步再编辑访问策略")
			}
			desired, saveErr := s.store.SaveDesired(site.ID, p, p.Version)
			if errors.Is(saveErr, ErrVersionConflict) {
				return app.ErrConflictMsg("访问策略已被其他操作更新，请刷新后重试")
			}
			if saveErr != nil {
				return saveErr
			}
			p = *desired
			desiredSaved = true
		} else if mark {
			current, readErr := s.store.Get(site.ID)
			if readErr != nil {
				return readErr
			}
			if current == nil || current.Version != p.Version {
				return app.ErrConflictMsg("访问策略已更新，请刷新后同步")
			}
		}
		changes, err := s.buildFiles(site, p, compiled, proxies, authFiles, accessPath, patched)
		if err != nil {
			return err
		}
		response, err := s.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{OperationID: opID, Changes: changes, TestNginx: true, ReloadNginx: site.Status == "enabled", TimeoutSeconds: 60})
		if err == nil && response == nil {
			err = fmt.Errorf("Agent 未返回文件事务确认")
		}
		if err == nil && mark {
			err = s.store.MarkApplied(site.ID, p.Version)
		} else if err == nil {
			current, readErr := s.store.Get(site.ID)
			if readErr != nil {
				return readErr
			}
			if current != nil && current.Version == p.Version {
				err = s.store.MarkApplied(site.ID, p.Version)
			}
		}
		return err
	}
	var err error
	if s.proxy != nil {
		err = s.proxy.WithAccessPolicyConfig(ctx, site.ID, apply)
	} else {
		var main []byte
		main, _, err = s.agent.ReadFile(ctx, site.ConfigPath)
		if err == nil {
			err = apply(main)
		}
	}
	if err != nil {
		_ = s.operations.UpdateError(opID, "failed", app.ErrAgentUnavailable, err.Error(), "")
		if desiredSaved || !save {
			_ = s.store.MarkError(site.ID, p.Version, err.Error())
		}
		if appErr, ok := err.(*app.AppError); ok && (appErr.Code == app.ErrConflict || appErr.Code == app.ErrValidationFailed) {
			if desiredSaved {
				return app.NewAppError(appErr.Code, appErr.Message, map[string]any{"sync_required": true, "desired_saved": true, "saved_version": p.Version, "operation_id": opID})
			}
			return err
		}
		return app.NewAppError(app.ErrAgentUnavailable, "访问策略未确认生效："+err.Error(), map[string]any{"sync_required": desiredSaved || !save, "operation_id": opID, "desired_saved": desiredSaved, "saved_version": p.Version})
	}
	_ = s.operations.UpdateStatus(opID, "success")
	return nil
}

func (s *Service) buildFiles(site *repo.Site, p Policy, compiled *CompiledPolicy, proxies []string, authFiles map[string]string, accessPath string, patched []byte) ([]agentclient.FileChangeRequest, error) {
	server := compiled.ServerContent
	for _, proxy := range proxies {
		if _, err := netip.ParsePrefix(proxy); err != nil {
			if _, addrErr := netip.ParseAddr(proxy); addrErr != nil {
				return nil, fmt.Errorf("可信代理格式无效")
			}
		}
		server = "set_real_ip_from " + proxy + ";\n" + server
	}
	if len(proxies) > 0 {
		server = "real_ip_header X-Forwarded-For;\nreal_ip_recursive on;\n" + server
	}
	files := make(map[string]string, len(compiled.Files)+4)
	for path, content := range compiled.Files {
		files[path] = content
	}
	files[s.globalPath(site.ID)] = compiled.GlobalContent
	files[accessPath] = server
	files[site.ConfigPath] = string(patched)
	if site.HotlinkPath != "" {
		files[site.HotlinkPath] = "# Managed by unified access policy.\n"
	}
	accounts, err := s.backupAccounts(p)
	if err != nil {
		return nil, err
	}
	bundle, err := json.Marshal(backupBundle{Policy: p, AuthFiles: authFiles, Accounts: accounts})
	if err != nil {
		return nil, err
	}
	if len(bundle) > MaxRenderedBytes {
		return nil, app.ErrValidationFailedMsg("访问策略快照超过 64 MiB", nil)
	}
	files[s.bundlePath(site.ID)] = string(bundle)
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	changes := make([]agentclient.FileChangeRequest, 0, len(paths)*2)
	dirs := map[string]bool{}
	for _, path := range paths {
		dir := filepath.Dir(path)
		if !dirs[dir] {
			changes = append(changes, agentclient.FileChangeRequest{Type: "mkdir", Path: dir, Perm: 0755})
			dirs[dir] = true
		}
		perm := uint32(0644)
		if path == s.bundlePath(site.ID) {
			perm = 0600
		}
		changes = append(changes, policyFile(path, files[path], perm))
	}

	return changes, nil
}

func (s *Service) globalPath(siteID string) string {
	return filepath.Join(s.panelDir, "conf.d", "20-nxpanel-access-policy-"+siteID+".conf")
}
func (s *Service) bundlePath(siteID string) string {
	return filepath.Join(s.panelDir, "access-policy", siteID, "snapshot.json")
}

// RefreshActive only refreshes the applied snapshot; dependency updates must
// never publish a pending editor draft or activate a legacy site implicitly.
func (s *Service) RefreshActive(ctx context.Context, requestID string) error {
	items, err := s.store.List()
	if err != nil {
		return err
	}
	var errs []error
	for _, item := range items {
		if !s.Managed(item.SiteID) {
			continue
		}
		p, err := s.store.GetApplied(item.SiteID)
		if err == nil && p != nil {
			err = s.applyPolicy(ctx, item.SiteID, *p, requestID, false)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", item.SiteID, err))
		}
	}
	return errors.Join(errs...)
}

// Owned access blocks and HTTPS redirection have known semantics. For other
// directives tokenize statements, rather than checking line prefixes (a custom
// location commonly fits on one line).
