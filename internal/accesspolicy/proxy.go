package accesspolicy

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// PrepareProxyChange is called while the proxy service holds its writer lock.
// The returned completion must be called exactly once, including failure paths.
func (s *Service) PrepareProxyChange(ctx context.Context, site *repo.Site, proxies []*repo.SiteProxy, changes []agentclient.FileChangeRequest) ([]agentclient.FileChangeRequest, func(error) error, error) {
	s.mu.Lock()
	fail := func(err error) ([]agentclient.FileChangeRequest, func(error) error, error) {
		s.mu.Unlock()
		return nil, nil, err
	}
	p, err := s.store.Get(site.ID)
	if err != nil {
		return fail(err)
	}
	if p == nil || p.Mode != "unified" {
		s.mu.Unlock()
		return changes, func(error) error { return nil }, nil
	}
	if p.ApplyStatus != "applied" && p.PendingSource != "proxy" {
		return fail(app.ErrConflictMsg("访问策略有未完成的应用，请先在访问限制中同步，再修改反代"))
	}
	byID := map[string]*repo.SiteProxy{}
	for _, proxy := range proxies {
		byID[proxy.ID] = proxy
	}
	rules := make([]Rule, 0, len(p.Rules))
	for _, rule := range p.Rules {
		if rule.SourceType == "proxy" {
			proxy := byID[rule.SourceID]
			if proxy == nil {
				continue
			}
			// Linked proxy rules retain their user-defined place and action.
			// Only the path supplied by the associated proxy follows routing edits.
			if len(rule.Conditions) == 0 || rule.Conditions[0].Kind != "path" || rule.Match != "all" {
				return fail(app.ErrValidationFailedMsg("关联反代规则需要全部满足及首个路径条件，请在访问限制中重新关联", nil))
			}
			rule.Conditions[0] = Condition{Kind: "path", Operator: "prefix", Values: []string{proxy.LocationPath}}
			rule.SourceDisabled = !proxy.Enabled
		}
		rules = append(rules, rule)
	}
	p.Rules = rules
	p.PendingSource = "proxy"
	renderCtx, trusted, err := s.renderContext(site, *p)
	if err != nil {
		return fail(err)
	}
	compiled, err := Compile(site.ID, s.panelDir, *p, renderCtx)
	if err != nil {
		return fail(err)
	}
	mainIndex := -1
	var main []byte
	for i, change := range changes {
		if change.Path == site.ConfigPath && change.Type == "write" {
			mainIndex = i
			main, err = decodeFile(change)
			if err != nil {
				return fail(err)
			}
			break
		}
	}
	if mainIndex < 0 {
		return fail(fmt.Errorf("反代事务缺少主配置"))
	}
	if err := s.CheckConfig(ctx, site, main); err != nil {
		return fail(err)
	}
	desired, err := s.store.SaveDesired(site.ID, *p, p.Version)
	if err != nil {
		return fail(err)
	}
	accessPath := site.AccessLimitPath
	if accessPath == "" {
		accessPath = filepath.Join(s.panelDir, "access-limit", site.PrimaryDomain+".conf")
	}
	policyChanges, err := s.buildFiles(site, *desired, compiled, trusted, renderCtx.AuthFiles, accessPath, main)
	if err != nil {
		_ = s.store.MarkError(site.ID, desired.Version, err.Error())
		return fail(err)
	}
	// Last writer for a path is the unified compiler; retain independent proxy
	// cache/upstream changes from the existing transaction.
	byPath := map[string]bool{}
	for _, c := range policyChanges {
		if c.Type != "mkdir" {
			byPath[c.Path] = true
		}
	}
	merged := make([]agentclient.FileChangeRequest, 0, len(changes)+len(policyChanges))
	for _, c := range changes {
		if !byPath[c.Path] {
			merged = append(merged, c)
		}
	}
	merged = append(merged, policyChanges...)
	finish := func(applyErr error) error {
		defer s.mu.Unlock()
		if applyErr != nil {
			return s.store.MarkError(site.ID, desired.Version, applyErr.Error())
		}
		return s.store.MarkApplied(site.ID, desired.Version)
	}
	return merged, finish, nil
}

func decodeFile(change agentclient.FileChangeRequest) ([]byte, error) {
	return base64.StdEncoding.DecodeString(change.ContentBase64)
}

// resolveSources overwrites client-controlled source applicability with current
// proxy state while retaining the user's independent rule Enabled preference.
func (s *Service) resolveSources(siteID string, p *Policy) error {
	proxies, err := repo.NewProxyRepo(s.store.db).ListBySiteID(siteID)
	if err != nil {
		return err
	}
	byID := map[string]*repo.SiteProxy{}
	for _, proxy := range proxies {
		byID[proxy.ID] = proxy
	}
	for i := range p.Rules {
		rule := &p.Rules[i]
		rule.SourceDisabled = false
		if rule.SourceType != "proxy" {
			continue
		}
		proxy := byID[rule.SourceID]
		if proxy == nil {
			return app.ErrValidationFailedMsg("关联的反代已删除，请删除规则或解除关联", nil)
		}
		if rule.Match != "all" || len(rule.Conditions) == 0 || rule.Conditions[0].Kind != "path" {
			return app.ErrValidationFailedMsg("关联反代规则需要全部满足及首个路径条件", nil)
		}
		path := rule.Conditions[0]
		if path.Operator != "prefix" || path.Negate || len(path.Values) != 1 || path.Values[0] != proxy.LocationPath {
			return app.ErrValidationFailedMsg("关联反代路径已变化或不匹配，请刷新策略；修改路径条件前请解除反代关联", nil)
		}
		rule.SourceDisabled = !proxy.Enabled
	}
	return nil
}

// ProxyWriteGuard runs under proxy.writeMu before its repository is mutated.
func (s *Service) ProxyWriteGuard(siteID string, sync bool) error {
	p, err := s.store.Get(siteID)
	if err != nil {
		return err
	}
	if p == nil || p.Mode == "unified" && p.ApplyStatus == "applied" {
		return nil
	}
	if p.Mode == "unified" && p.PendingSource == "proxy" && sync {
		return nil
	}
	return app.ErrConflictMsg("访问策略尚未确认生效，请先在访问限制中同步，再修改反代")
}
