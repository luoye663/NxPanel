// Package maintenance applies removable site-local maintenance configuration.
// Desired state is durable before Agent apply; unknown results remain pending
// and can be reconciled against current files without restoring stale snapshots.
package maintenance

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

const MaxHTMLBytes = 256 * 1024
const BuiltinHTML = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>网站维护中</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f4f6f8;color:#243447;font-family:sans-serif}main{max-width:38rem;padding:2rem}h1{font-size:2rem}p{line-height:1.8}</style><main><h1>网站维护中</h1><p>我们正在进行维护，请稍后重试。</p></main></html>`

type Agent interface {
	NginxDump(context.Context, *agentclient.NginxDumpRequest) (*agentclient.NginxDumpResponse, error)
	ReadFile(context.Context, string) ([]byte, string, error)
	ApplyTransaction(context.Context, *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
}
type Service struct {
	mu         sync.Mutex
	store      *repo.MaintenanceRepo
	sites      *repo.SiteRepo
	operations *repo.OperationRepo
	agent      Agent
	panelDir   string
}

func NewService(store *repo.MaintenanceRepo, sites *repo.SiteRepo, ops *repo.OperationRepo, agent Agent, panelDir string) *Service {
	return &Service{store: store, sites: sites, operations: ops, agent: agent, panelDir: panelDir}
}

type Status struct {
	*repo.MaintenanceState
	FileHash       string `json:"file_hash"`
	EffectiveHTML  string `json:"effective_html"`
	Inherits       bool   `json:"inherits"`
	Drifted        bool   `json:"drifted"`
	ReadOnlyReason string `json:"read_only_reason,omitempty"`
}
type Defaults struct {
	*repo.MaintenanceDefaults
	EffectiveHTML string `json:"effective_html"`
	BuiltinHTML   string `json:"builtin_html"`
}
type UpdateRequest struct {
	Enabled      bool    `json:"enabled"`
	HTMLOverride *string `json:"html_override"`
	Version      int64   `json:"version"`
	ExpectedHash string  `json:"expected_hash"`
}
type DefaultsRequest struct {
	HTML    *string `json:"html"`
	Version int64   `json:"version"`
}

func validateHTML(html *string) error {
	if html != nil && (len(*html) > MaxHTMLBytes || !utf8.ValidString(*html) || strings.ContainsRune(*html, 0)) {
		return app.NewAppError(app.ErrValidationFailed, "维护页面必须是有效 UTF-8 HTML，且不超过 256 KiB", nil)
	}
	return nil
}
func effective(html *string) string {
	if html == nil {
		return BuiltinHTML
	}
	return *html
}
func (s *Service) Defaults(ctx context.Context) (*Defaults, error) {
	d, err := s.store.Defaults(ctx)
	if err != nil {
		return nil, err
	}
	return &Defaults{MaintenanceDefaults: d, EffectiveHTML: effective(d.HTML), BuiltinHTML: BuiltinHTML}, nil
}
func (s *Service) pagePath(id string) string {
	return filepath.Join(s.panelDir, "maintenance", id, "index.html")
}

func (s *Service) dependencies(ctx context.Context, content string) ([]agentclient.FileChangeRequest, error) {
	paths, err := nginxpatch.MaintenanceIncludePaths(content)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	total := len(content)
	var checks []agentclient.FileChangeRequest
	for len(paths) > 0 {
		path := paths[0]
		paths = paths[1:]
		if seen[path] || len(seen) >= 16 {
			return nil, fmt.Errorf("include ownership is cyclic, repeated or exceeds safety budget")
		}
		seen[path] = true
		data, _, err := s.agent.ReadFile(ctx, path)
		if err != nil {
			return nil, fmt.Errorf("include cannot be read through Agent path policy")
		}
		total += len(data)
		if total > nginxpatch.MaxConfigBytes {
			return nil, fmt.Errorf("include graph exceeds safety budget")
		}
		more, err := nginxpatch.MaintenanceIncludePaths(string(data))
		if err != nil {
			return nil, err
		}
		paths = append(paths, more...)
		checks = append(checks, agentclient.FileChangeRequest{Type: "check", Path: path, FollowSymlink: true, ExpectedHash: nginxpatch.Hash(string(data))})
	}
	return checks, nil
}
func (s *Service) read(ctx context.Context, id string) (*repo.Site, *repo.MaintenanceState, string, error) {
	site, err := s.sites.GetByIDContext(ctx, id)
	if err != nil {
		return nil, nil, "", err
	}
	if site == nil {
		return nil, nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	state, err := s.store.Get(ctx, id)
	if err != nil {
		return nil, nil, "", err
	}
	data, _, err := s.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return nil, nil, "", app.NewAppError(app.ErrAgentUnavailable, "读取站点配置失败，请检查 Agent 白名单", nil)
	}
	return site, state, string(data), nil
}
func (s *Service) Status(ctx context.Context, id string) (*Status, error) {
	site, state, content, err := s.read(ctx, id)
	if err != nil {
		return nil, err
	}
	d, err := s.Defaults(ctx)
	if err != nil {
		return nil, err
	}
	html := state.HTMLOverride
	if html == nil {
		html = &d.EffectiveHTML
	}
	result := &Status{MaintenanceState: state, FileHash: nginxpatch.Hash(content), EffectiveHTML: *html, Inherits: state.HTMLOverride == nil}
	var domains []string
	_ = json.Unmarshal([]byte(site.DomainsJSON), &domains)
	want, err := nginxpatch.SetMaintenance(content, site.PrimaryDomain, domains, id, s.pagePath(id), true)
	if err != nil {
		result.ReadOnlyReason = err.Error()
		return result, nil
	}
	if _, err := s.dependencies(ctx, content); err != nil {
		result.ReadOnlyReason = err.Error()
		return result, nil
	}
	if state.DesiredEnabled {
		result.Drifted = want != content
	} else {
		stripped, err := nginxpatch.SetMaintenance(content, site.PrimaryDomain, domains, id, s.pagePath(id), false)
		result.Drifted = err != nil || stripped != content
	}
	if state.DesiredEnabled && !result.Drifted {
		page, _, readErr := s.agent.ReadFile(ctx, s.pagePath(id))
		result.Drifted = readErr != nil || string(page) != result.EffectiveHTML
	}
	return result, nil
}
func (s *Service) Update(ctx context.Context, id string, req *UpdateRequest, requestID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.update(ctx, id, req, requestID)
}
func (s *Service) update(ctx context.Context, id string, req *UpdateRequest, requestID string) (string, error) {
	if err := validateHTML(req.HTMLOverride); err != nil {
		return "", err
	}
	site, state, content, err := s.read(ctx, id)
	if err != nil {
		return "", err
	}
	if req.Version != state.Version || req.ExpectedHash == "" || req.ExpectedHash != nginxpatch.Hash(content) {
		return "", app.NewAppError(app.ErrConflict, "配置或维护状态已变化，请刷新后重试", nil)
	}
	if req.Enabled && site.Status != "enabled" {
		return "", app.NewAppError(app.ErrConflict, "仅启用状态的网站可以进入维护", nil)
	}
	var domains []string
	_ = json.Unmarshal([]byte(site.DomainsJSON), &domains)
	patched, err := nginxpatch.SetMaintenance(content, site.PrimaryDomain, domains, id, s.pagePath(id), req.Enabled)
	if err != nil {
		return "", app.NewAppError(app.ErrValidationFailed, "不能安全定位维护配置："+err.Error(), nil)
	}
	var dependencies []agentclient.FileChangeRequest
	if req.Enabled {
		dump, dumpErr := s.agent.NginxDump(ctx, &agentclient.NginxDumpRequest{})
		if dumpErr != nil {
			return "", app.NewAppError(app.ErrAgentUnavailable, "无法检查全局运行配置，未进入维护", nil)
		}
		if strings.Contains(strings.ToLower(dump.Stderr), "conflicting server name") {
			return "", app.NewAppError(app.ErrConflict, "全局 server_name 存在歧义，拒绝猜测维护目标", nil)
		}
		if err := nginxpatch.ValidateMaintenanceRuntime(dump.Stdout); err != nil {
			return "", app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
		dependencies, err = s.dependencies(ctx, content)
		if err != nil {
			return "", app.NewAppError(app.ErrValidationFailed, "不能安全启用维护："+err.Error(), nil)
		}
	}
	d, err := s.Defaults(ctx)
	if err != nil {
		return "", err
	}
	html := req.HTMLOverride
	if html == nil {
		html = &d.EffectiveHTML
	}
	opID := app.NewOperationID()
	op := &repo.Operation{ID: opID, Action: "site.maintenance", TargetType: "site", TargetID: id, RequestID: requestID, Message: "更新网站维护模式（局部配置）"}
	if err := s.store.Begin(ctx, id, req.Enabled, req.HTMLOverride, req.Version, op); err != nil {
		if errors.Is(err, repo.ErrMaintenanceConflict) {
			return "", app.NewAppError(app.ErrConflict, err.Error(), nil)
		}
		return "", app.NewAppError(app.ErrInternalError, "保存维护目标状态失败", nil)
	}
	if !req.Enabled && !state.Enabled && state.ApplyStatus == "synced" && patched == content {
		if err := s.store.Confirm(ctx, id, req.Version+1, false, opID); err != nil {
			return opID, app.NewAppError(app.ErrInternalError, "页面设置待确认，请刷新后对账", map[string]any{"sync_required": true, "operation_id": opID})
		}
		return opID, nil
	}
	changes := []agentclient.FileChangeRequest{{Type: "write", Path: site.ConfigPath, ContentBase64: base64.StdEncoding.EncodeToString([]byte(patched)), ExpectedHash: req.ExpectedHash, MaintenanceTransition: true, FollowSymlink: true}}
	if req.Enabled {
		changes = append([]agentclient.FileChangeRequest{{Type: "write", Path: s.pagePath(id), ContentBase64: base64.StdEncoding.EncodeToString([]byte(*html)), Perm: 0644}}, changes...)
	}
	_, err = s.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{OperationID: opID, Changes: append(dependencies, changes...), TestNginx: true, ReloadNginx: true})
	if err != nil {
		_ = s.operations.UpdateError(opID, "failed", app.ErrAgentUnavailable, "Agent 应用失败或结果不确定；保留待对账状态", "")
		return opID, app.NewAppError(app.ErrAgentUnavailable, "维护配置未能确认生效，请刷新并执行对账或恢复访问", map[string]any{"sync_required": true, "operation_id": opID})
	}
	if err := s.store.Confirm(ctx, id, req.Version+1, req.Enabled, opID); err != nil {
		return opID, app.NewAppError(app.ErrInternalError, "配置已应用，数据库确认失败，请执行对账", map[string]any{"sync_required": true, "operation_id": opID})
	}
	return opID, nil
}

func (s *Service) SetDefaults(ctx context.Context, req *DefaultsRequest, requestID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateHTML(req.HTML); err != nil {
		return "", err
	}
	opID := app.NewOperationID()
	ids, err := s.store.SetDefaults(ctx, req.HTML, req.Version, &repo.Operation{ID: opID, Action: "maintenance.defaults", TargetType: "settings", TargetID: "maintenance", RequestID: requestID, Message: "更新全局维护页面"})
	if err != nil {
		if errors.Is(err, repo.ErrMaintenanceConflict) {
			return "", app.NewAppError(app.ErrConflict, "全局维护页面已变化，请刷新", nil)
		}
		return "", err
	}
	var failed []string
	for _, id := range ids {
		_, state, content, err := s.read(ctx, id)
		if err != nil {
			failed = append(failed, id)
			continue
		}
		_, err = s.update(ctx, id, &UpdateRequest{Enabled: state.DesiredEnabled, HTMLOverride: state.HTMLOverride, Version: state.Version, ExpectedHash: nginxpatch.Hash(content)}, requestID)
		if err != nil {
			failed = append(failed, id)
		}
	}
	if len(failed) > 0 {
		_ = s.operations.UpdateError(opID, "failed", app.ErrAgentUnavailable, "部分站点待对账", "")
		return opID, app.NewAppError(app.ErrAgentUnavailable, "全局页面已保存，部分继承站点待对账", map[string]any{"sync_required": true, "site_ids": failed, "operation_id": opID})
	}
	if err := s.operations.UpdateStatus(opID, "success"); err != nil {
		return opID, err
	}
	return opID, nil
}
