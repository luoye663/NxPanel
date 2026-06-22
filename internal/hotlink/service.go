package hotlink

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/security"
)

type siteRepo interface {
	GetByID(id string) (*repo.Site, error)
	Update(site *repo.Site) error
}

type opRepo interface {
	Create(o *repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}

type agentClient interface {
	ApplyTransaction(ctx context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
	ReadFile(ctx context.Context, path string) ([]byte, string, error)
}

type Service struct {
	siteRepo siteRepo
	ruleRepo *repo.HotlinkRuleRepo
	opRepo   opRepo
	agent    agentClient
	panelDir string
}

type RuleResponse struct {
	ID                string   `json:"id"`
	SiteID            string   `json:"site_id"`
	Name              string   `json:"name"`
	Enabled           bool     `json:"enabled"`
	Extensions        []string `json:"extensions"`
	Referers          []string `json:"referers"`
	AllowEmptyReferer bool     `json:"allow_empty_referer"`
	BlockStatus       int      `json:"block_status"`
	SortOrder         int      `json:"sort_order"`
	CreatedAt         string   `json:"created_at"`
	UpdatedAt         string   `json:"updated_at"`
}

type SaveRuleRequest struct {
	Name              string   `json:"name"`
	Enabled           *bool    `json:"enabled"`
	Extensions        []string `json:"extensions"`
	Referers          []string `json:"referers"`
	AllowEmptyReferer *bool    `json:"allow_empty_referer"`
	BlockStatus       int      `json:"block_status"`
}

var extensionRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func NewService(siteRepo siteRepo, ruleRepo *repo.HotlinkRuleRepo, opRepo opRepo, agent agentClient, panelDir string) *Service {
	return &Service{siteRepo: siteRepo, ruleRepo: ruleRepo, opRepo: opRepo, agent: agent, panelDir: panelDir}
}

func (svc *Service) List(siteID string) ([]*RuleResponse, error) {
	if err := svc.ensureSiteExists(siteID); err != nil {
		return nil, err
	}
	rules, err := svc.ruleRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	result := make([]*RuleResponse, 0, len(rules))
	for _, rule := range rules {
		result = append(result, toResponse(rule))
	}
	return result, nil
}

func (svc *Service) Create(ctx context.Context, siteID string, req *SaveRuleRequest, requestID string) (*RuleResponse, error) {
	site, err := svc.loadWritableSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	rule, err := svc.normalizeRequest(siteID, req, nil)
	if err != nil {
		return nil, err
	}
	rule.ID = app.NewID("hotlink")

	if err := svc.ruleRepo.Create(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.renderAndApply(ctx, site, requestID, "hotlink.create", "创建防盗链规则: "+rule.Name); err != nil {
		_ = svc.ruleRepo.Delete(rule.ID)
		return nil, err
	}
	return toResponse(rule), nil
}

func (svc *Service) Update(ctx context.Context, siteID, ruleID string, req *SaveRuleRequest, requestID string) (*RuleResponse, error) {
	site, err := svc.loadWritableSite(ctx, siteID)
	if err != nil {
		return nil, err
	}
	existing, err := svc.ruleRepo.GetByID(ruleID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if existing == nil || existing.SiteID != siteID {
		return nil, app.NewAppError(app.ErrNotFound, "防盗链规则不存在", nil)
	}
	rule, err := svc.normalizeRequest(siteID, req, existing)
	if err != nil {
		return nil, err
	}
	rule.ID = ruleID
	rule.SortOrder = existing.SortOrder
	if err := svc.ruleRepo.Update(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.renderAndApply(ctx, site, requestID, "hotlink.update", "更新防盗链规则: "+rule.Name); err != nil {
		return nil, err
	}
	return toResponse(rule), nil
}

func (svc *Service) Delete(ctx context.Context, siteID, ruleID, requestID string) error {
	site, err := svc.loadWritableSite(ctx, siteID)
	if err != nil {
		return err
	}
	rule, err := svc.ruleRepo.GetByID(ruleID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if rule == nil || rule.SiteID != siteID {
		return app.NewAppError(app.ErrNotFound, "防盗链规则不存在", nil)
	}
	if err := svc.ruleRepo.Delete(ruleID); err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return svc.renderAndApply(ctx, site, requestID, "hotlink.delete", "删除防盗链规则: "+rule.Name)
}

func (svc *Service) normalizeRequest(siteID string, req *SaveRuleRequest, existing *repo.SiteHotlinkRule) (*repo.SiteHotlinkRule, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" && existing != nil {
		name = existing.Name
	}
	if name == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "规则名称不能为空", nil)
	}
	extensions := splitCSV("")
	if existing != nil {
		extensions = splitCSV(existing.Extensions)
	}
	if len(req.Extensions) > 0 || existing == nil {
		var err error
		extensions, err = normalizeExtensions(req.Extensions)
		if err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	}
	referers := splitCSV("")
	if existing != nil {
		referers = splitCSV(existing.Referers)
	}
	if len(req.Referers) > 0 || existing == nil {
		var err error
		referers, err = normalizeReferers(req.Referers)
		if err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	}
	allowEmpty := true
	if existing != nil {
		allowEmpty = existing.AllowEmptyReferer
	}
	if req.AllowEmptyReferer != nil {
		allowEmpty = *req.AllowEmptyReferer
	}
	if len(referers) == 0 && !allowEmpty {
		return nil, app.NewAppError(app.ErrValidationFailed, "拒绝空 Referer 时必须至少配置一个 Referer 白名单", nil)
	}
	blockStatus := req.BlockStatus
	if blockStatus == 0 && existing != nil {
		blockStatus = existing.BlockStatus
	}
	if blockStatus == 0 {
		blockStatus = 403
	}
	if blockStatus != 403 && blockStatus != 404 && blockStatus != 444 {
		return nil, app.NewAppError(app.ErrValidationFailed, "拦截状态码只允许 403、404、444", nil)
	}
	enabled := true
	if existing != nil {
		enabled = existing.Enabled
	}
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return &repo.SiteHotlinkRule{
		SiteID:            siteID,
		Name:              name,
		Enabled:           enabled,
		Extensions:        strings.Join(extensions, ","),
		Referers:          strings.Join(referers, ","),
		AllowEmptyReferer: allowEmpty,
		BlockStatus:       blockStatus,
	}, nil
}

func (svc *Service) renderAndApply(ctx context.Context, site *repo.Site, requestID, action, message string) error {
	rules, err := svc.ruleRepo.ListBySiteID(site.ID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := rejectExtensionOverlap(rules); err != nil {
		return app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	content := renderHotlinkInclude(rules)

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: action, TargetType: "site", TargetID: site.ID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("%s 站点 %s", message, site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	changes := []agentclient.FileChangeRequest{
		{Type: "mkdir", Path: filepath.Dir(site.HotlinkPath), Perm: 0755},
		{Type: "write", Path: site.HotlinkPath, ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)), Perm: 0644},
	}
	if configContent, _, readErr := svc.agent.ReadFile(ctx, site.ConfigPath); readErr == nil {
		markerBody := []byte("    include " + site.HotlinkPath + ";\n")
		patched, injectErr := nginx.EnsureMarkerBlock(configContent, nginx.MarkerNameHotlink, markerBody)
		if injectErr != nil {
			_ = svc.opRepo.UpdateError(opID, "failed", app.ErrValidationFailed, injectErr.Error(), "")
			return app.NewAppError(app.ErrValidationFailed, "防盗链标识块注入失败: "+injectErr.Error(), nil)
		}
		changes = append(changes, agentclient.FileChangeRequest{
			Type:          "write",
			Path:          site.ConfigPath,
			ContentBase64: base64.StdEncoding.EncodeToString(patched),
			Perm:          0644,
		})
	} else {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, readErr.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "读取配置文件失败: "+readErr.Error(), nil)
	}

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "应用防盗链配置失败: "+agentErr.Error(), nil)
	}
	_ = svc.opRepo.UpdateStatus(opID, "success")
	return nil
}

func (svc *Service) loadWritableSite(ctx context.Context, siteID string) (*repo.Site, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	if site.HotlinkPath == "" {
		site.HotlinkPath = filepath.Join(svc.panelDir, "hotlink", site.PrimaryDomain+".conf")
		_ = svc.siteRepo.Update(site)
	}
	return site, nil
}

func (svc *Service) ensureSiteExists(siteID string) error {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	return nil
}

func normalizeExtensions(raw []string) ([]string, error) {
	seen := map[string]bool{}
	for _, item := range raw {
		for _, part := range strings.FieldsFunc(item, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
			ext := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(part), "."))
			if ext == "" {
				continue
			}
			if !extensionRE.MatchString(ext) {
				return nil, fmt.Errorf("后缀 %s 不合法", ext)
			}
			seen[ext] = true
		}
	}
	if len(seen) == 0 || len(seen) > 64 {
		return nil, fmt.Errorf("后缀数量必须为 1-64 个")
	}
	extensions := make([]string, 0, len(seen))
	for ext := range seen {
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)
	return extensions, nil
}

func normalizeReferers(raw []string) ([]string, error) {
	seen := map[string]bool{}
	for _, item := range raw {
		for _, part := range strings.FieldsFunc(item, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == '\t' }) {
			ref := strings.TrimSpace(part)
			if ref == "" {
				continue
			}
			if strings.ContainsAny(ref, ";{}\"\r\x00") || strings.Contains(ref, "://") || strings.Contains(ref, "/") {
				return nil, fmt.Errorf("Referer %s 不合法", ref)
			}
			if ref != "server_names" && ref != "blocked" {
				domain := strings.TrimPrefix(ref, "*.")
				if err := security.ValidateDomain(domain); err != nil {
					return nil, fmt.Errorf("Referer %s 不合法: %w", ref, err)
				}
			}
			seen[ref] = true
		}
	}
	referers := make([]string, 0, len(seen))
	for ref := range seen {
		referers = append(referers, ref)
	}
	sort.Strings(referers)
	return referers, nil
}

func rejectExtensionOverlap(rules []*repo.SiteHotlinkRule) error {
	owner := map[string]string{}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		for _, ext := range splitCSV(rule.Extensions) {
			if prev := owner[ext]; prev != "" {
				return fmt.Errorf("启用规则 %s 与 %s 存在重复后缀 %s", prev, rule.Name, ext)
			}
			owner[ext] = rule.Name
		}
	}
	return nil
}

func renderHotlinkInclude(rules []*repo.SiteHotlinkRule) string {
	var buf strings.Builder
	buf.WriteString("# generated by nxpanel, do not edit manually\n")
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		extensions := splitCSV(rule.Extensions)
		if len(extensions) == 0 {
			continue
		}
		buf.WriteString("\n# ")
		buf.WriteString(rule.Name)
		buf.WriteString("\n")
		buf.WriteString("location ~* \\.(")
		buf.WriteString(strings.Join(extensions, "|"))
		buf.WriteString(")$ {\n")
		buf.WriteString("    valid_referers")
		if rule.AllowEmptyReferer {
			buf.WriteString(" none")
		}
		for _, ref := range splitCSV(rule.Referers) {
			buf.WriteString(" ")
			buf.WriteString(ref)
		}
		buf.WriteString(";\n")
		buf.WriteString("    if ($invalid_referer) {\n")
		fmt.Fprintf(&buf, "        return %d;\n", rule.BlockStatus)
		buf.WriteString("    }\n")
		buf.WriteString("}\n")
	}
	return buf.String()
}

func splitCSV(value string) []string {
	var result []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func toResponse(rule *repo.SiteHotlinkRule) *RuleResponse {
	return &RuleResponse{
		ID:                rule.ID,
		SiteID:            rule.SiteID,
		Name:              rule.Name,
		Enabled:           rule.Enabled,
		Extensions:        splitCSV(rule.Extensions),
		Referers:          splitCSV(rule.Referers),
		AllowEmptyReferer: rule.AllowEmptyReferer,
		BlockStatus:       rule.BlockStatus,
		SortOrder:         rule.SortOrder,
		CreatedAt:         rule.CreatedAt,
		UpdatedAt:         rule.UpdatedAt,
	}
}
