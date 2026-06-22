package accesslimit

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

type siteGetter interface {
	GetByID(id string) (*repo.Site, error)
}

type opRecorder interface {
	Create(o *repo.Operation) error
	UpdateStatus(id, status string) error
	UpdateError(id, status, errorCode, errorMessage, stderr string) error
}

type agentTx interface {
	ApplyTransaction(ctx context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error)
}

type configReader interface {
	ReadFile(ctx context.Context, path string) ([]byte, string, error)
}

type Service struct {
	siteRepo            siteGetter
	authRuleRepo        *repo.AuthRuleRepo
	denyRuleRepo        *repo.DenyRuleRepo
	ipWhitelistRuleRepo *repo.IPWhitelistRuleRepo
	opRepo              opRecorder
	agent               agentTx
	configReader        configReader
	panelDir            string
}

func NewService(
	siteRepo siteGetter,
	authRuleRepo *repo.AuthRuleRepo,
	denyRuleRepo *repo.DenyRuleRepo,
	ipWhitelistRuleRepo *repo.IPWhitelistRuleRepo,
	opRepo opRecorder,
	agent agentTx,
	cr configReader,
	panelDir string,
) *Service {
	return &Service{
		siteRepo:            siteRepo,
		authRuleRepo:        authRuleRepo,
		denyRuleRepo:        denyRuleRepo,
		ipWhitelistRuleRepo: ipWhitelistRuleRepo,
		opRepo:              opRepo,
		agent:               agent,
		configReader:        cr,
		panelDir:            panelDir,
	}
}

type AuthRuleResponse struct {
	ID        string `json:"id"`
	SiteID    string `json:"site_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Username  string `json:"username"`
	Enabled   bool   `json:"enabled"`
	SortOrder int    `json:"sort_order"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CreateAuthRuleRequest struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled"`
}

type UpdateAuthRuleRequest struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled"`
}

type DenyRuleResponse struct {
	ID               string `json:"id"`
	SiteID           string `json:"site_id"`
	Name             string `json:"name"`
	ExtensionPattern string `json:"extension_pattern"`
	PathPattern      string `json:"path_pattern"`
	Enabled          bool   `json:"enabled"`
	SortOrder        int    `json:"sort_order"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

type IPLimitRuleResponse struct {
	ID        string   `json:"id"`
	SiteID    string   `json:"site_id"`
	Name      string   `json:"name"`
	RuleType  string   `json:"rule_type"`
	IPs       []string `json:"ips"`
	Enabled   bool     `json:"enabled"`
	SortOrder int      `json:"sort_order"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

type CreateDenyRuleRequest struct {
	Name             string `json:"name"`
	ExtensionPattern string `json:"extension_pattern"`
	PathPattern      string `json:"path_pattern"`
	Enabled          *bool  `json:"enabled"`
}

type UpdateDenyRuleRequest struct {
	Name             string `json:"name"`
	ExtensionPattern string `json:"extension_pattern"`
	PathPattern      string `json:"path_pattern"`
	Enabled          *bool  `json:"enabled"`
}

type CreateIPLimitRuleRequest struct {
	Name     string   `json:"name"`
	RuleType string   `json:"rule_type"`
	IPs      []string `json:"ips"`
	Enabled  *bool    `json:"enabled"`
}

type UpdateIPLimitRuleRequest struct {
	Name     string   `json:"name"`
	RuleType string   `json:"rule_type"`
	IPs      []string `json:"ips"`
	Enabled  *bool    `json:"enabled"`
}

func authRuleToResponse(r *repo.SiteAuthRule) *AuthRuleResponse {
	return &AuthRuleResponse{
		ID:        r.ID,
		SiteID:    r.SiteID,
		Name:      r.Name,
		Path:      r.Path,
		Username:  r.Username,
		Enabled:   r.Enabled,
		SortOrder: r.SortOrder,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func denyRuleToResponse(r *repo.SiteDenyRule) *DenyRuleResponse {
	return &DenyRuleResponse{
		ID:               r.ID,
		SiteID:           r.SiteID,
		Name:             r.Name,
		ExtensionPattern: r.ExtensionPattern,
		PathPattern:      r.PathPattern,
		Enabled:          r.Enabled,
		SortOrder:        r.SortOrder,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}
}

func ipLimitRuleToResponse(r *repo.SiteIPWhitelistRule) *IPLimitRuleResponse {
	return &IPLimitRuleResponse{
		ID:        r.ID,
		SiteID:    r.SiteID,
		Name:      r.Name,
		RuleType:  r.RuleType,
		IPs:       repo.ParseJSONStringSlice(r.IPsJSON),
		Enabled:   r.Enabled,
		SortOrder: r.SortOrder,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
}

func (svc *Service) ListAuthRules(siteID string) ([]*AuthRuleResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	rules, err := svc.authRuleRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	result := make([]*AuthRuleResponse, 0, len(rules))
	for _, r := range rules {
		result = append(result, authRuleToResponse(r))
	}
	return result, nil
}

func (svc *Service) CreateAuthRule(ctx context.Context, siteID string, req *CreateAuthRuleRequest, requestID string) (*AuthRuleResponse, error) {
	if req.Name == "" || req.Path == "" || req.Username == "" || req.Password == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "名称、路径、用户名和密码不能为空", nil)
	}
	if err := ValidatePath(req.Path); err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	ruleID := NewRuleID()
	path := SanitizePath(req.Path)
	htpasswdPath := filepath.Join(svc.panelDir, "htpasswd", siteID+"_"+ruleID+".htpasswd")

	htpasswdContent := GenerateHtpasswdContent(req.Username, req.Password)

	rule := &repo.SiteAuthRule{
		ID:           ruleID,
		SiteID:       siteID,
		Name:         req.Name,
		Path:         path,
		Username:     req.Username,
		PasswordHash: htpasswdContent[:len(htpasswdContent)-1],
		HtpasswdPath: htpasswdPath,
		Enabled:      enabled,
	}

	if err := svc.authRuleRepo.Create(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	if err := svc.renderAndApply(ctx, site, requestID, "创建加密访问规则: "+req.Name, map[string]string{
		htpasswdPath: htpasswdContent,
	}); err != nil {
		_ = svc.authRuleRepo.Delete(ruleID)
		return nil, err
	}

	return authRuleToResponse(rule), nil
}

func (svc *Service) UpdateAuthRule(ctx context.Context, siteID, ruleID string, req *UpdateAuthRuleRequest, requestID string) (*AuthRuleResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	rule, err := svc.authRuleRepo.GetByID(ruleID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if rule == nil || rule.SiteID != siteID {
		return nil, app.NewAppError(app.ErrNotFound, "规则不存在", nil)
	}

	if req.Name != "" {
		rule.Name = req.Name
	}
	if req.Path != "" {
		if err := ValidatePath(req.Path); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
		rule.Path = SanitizePath(req.Path)
	}
	if req.Username != "" {
		rule.Username = req.Username
	}

	htpasswdUpdates := map[string]string{}
	if req.Password != "" {
		htpasswdContent := GenerateHtpasswdContent(req.Username, req.Password)
		rule.PasswordHash = htpasswdContent[:len(htpasswdContent)-1]
		htpasswdUpdates[rule.HtpasswdPath] = htpasswdContent
	} else if req.Username != "" {
		if rule.PasswordHash != "" {
			htpasswdUpdates[rule.HtpasswdPath] = rule.Username + ":" + strings.TrimPrefix(rule.PasswordHash, rule.Username+":") + "\n"
			rule.PasswordHash = rule.Username + ":" + strings.TrimPrefix(rule.PasswordHash, rule.Username+":")
		}
	}

	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}

	if err := svc.authRuleRepo.Update(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	if err := svc.renderAndApply(ctx, site, requestID, "更新加密访问规则: "+rule.Name, htpasswdUpdates); err != nil {
		return nil, err
	}

	return authRuleToResponse(rule), nil
}

func (svc *Service) DeleteAuthRule(ctx context.Context, siteID, ruleID string, requestID string) error {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	rule, err := svc.authRuleRepo.GetByID(ruleID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if rule == nil || rule.SiteID != siteID {
		return app.NewAppError(app.ErrNotFound, "规则不存在", nil)
	}

	htpasswdPath := rule.HtpasswdPath

	if err := svc.authRuleRepo.Delete(ruleID); err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	svc.ensureAccessLimitPath(site)

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.delete_auth_rule", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("删除加密访问规则: %s 站点 %s", rule.Name, site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	accessLimitContent := svc.buildAccessLimitContent(siteID)
	changes := []agentclient.FileChangeRequest{
		{
			Type:          "write",
			Path:          site.AccessLimitPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(accessLimitContent)),
			Perm:          0644,
		},
		{
			Type: "remove",
			Path: htpasswdPath,
		},
	}

	svc.maybeInjectAccessLimitMarker(ctx, site, &changes)

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "应用配置失败: "+agentErr.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")
	return nil
}

func (svc *Service) ListDenyRules(siteID string) ([]*DenyRuleResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	rules, err := svc.denyRuleRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	result := make([]*DenyRuleResponse, 0, len(rules))
	for _, r := range rules {
		result = append(result, denyRuleToResponse(r))
	}
	return result, nil
}

func (svc *Service) ListIPLimitRules(siteID string) ([]*IPLimitRuleResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	if svc.ipWhitelistRuleRepo == nil {
		return []*IPLimitRuleResponse{}, nil
	}
	rules, err := svc.ipWhitelistRuleRepo.ListBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	result := make([]*IPLimitRuleResponse, 0, len(rules))
	for _, r := range rules {
		result = append(result, ipLimitRuleToResponse(r))
	}
	return result, nil
}

func normalizeIPRuleType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "allow", "whitelist":
		return "allow", nil
	case "deny", "blacklist":
		return "deny", nil
	default:
		return "", fmt.Errorf("规则类型只能是 allow 或 deny")
	}
}

func (svc *Service) CreateDenyRule(ctx context.Context, siteID string, req *CreateDenyRuleRequest, requestID string) (*DenyRuleResponse, error) {
	extPattern := strings.TrimSpace(req.ExtensionPattern)
	pathPattern := strings.TrimSpace(req.PathPattern)

	if req.Name == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "名称不能为空", nil)
	}
	if extPattern == "" && pathPattern == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "后缀和路径至少填写一项", nil)
	}
	if extPattern != "" {
		if err := ValidateExtensionPattern(extPattern); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	}
	if pathPattern != "" {
		if err := ValidatePath(pathPattern); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	}

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if extPattern != "" {
		extPattern = cleanExtensionPattern(extPattern)
	}

	ruleID := NewDenyRuleID()
	rule := &repo.SiteDenyRule{
		ID:               ruleID,
		SiteID:           siteID,
		Name:             req.Name,
		DenyType:         "extension",
		Pattern:          extPattern,
		ExtensionPattern: extPattern,
		PathPattern:      pathPattern,
		Enabled:          enabled,
	}

	if err := svc.denyRuleRepo.Create(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	if err := svc.renderAndApply(ctx, site, requestID, "创建禁止访问规则: "+req.Name, nil); err != nil {
		_ = svc.denyRuleRepo.Delete(ruleID)
		return nil, err
	}

	return denyRuleToResponse(rule), nil
}

func (svc *Service) UpdateDenyRule(ctx context.Context, siteID, ruleID string, req *UpdateDenyRuleRequest, requestID string) (*DenyRuleResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	rule, err := svc.denyRuleRepo.GetByID(ruleID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if rule == nil || rule.SiteID != siteID {
		return nil, app.NewAppError(app.ErrNotFound, "规则不存在", nil)
	}

	if req.Name != "" {
		rule.Name = req.Name
	}
	if req.ExtensionPattern != "" {
		rule.ExtensionPattern = cleanExtensionPattern(req.ExtensionPattern)
		if err := ValidateExtensionPattern(rule.ExtensionPattern); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	}
	if req.PathPattern != "" {
		rule.PathPattern = strings.TrimSpace(req.PathPattern)
		if err := ValidatePath(rule.PathPattern); err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if rule.ExtensionPattern == "" && rule.PathPattern == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "后缀和路径至少填写一项", nil)
	}

	if err := svc.denyRuleRepo.Update(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	if err := svc.renderAndApply(ctx, site, requestID, "更新禁止访问规则: "+rule.Name, nil); err != nil {
		return nil, err
	}

	return denyRuleToResponse(rule), nil
}

func (svc *Service) DeleteDenyRule(ctx context.Context, siteID, ruleID string, requestID string) error {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	rule, err := svc.denyRuleRepo.GetByID(ruleID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if rule == nil || rule.SiteID != siteID {
		return app.NewAppError(app.ErrNotFound, "规则不存在", nil)
	}

	if err := svc.denyRuleRepo.Delete(ruleID); err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	if err := svc.renderAndApply(ctx, site, requestID, "删除禁止访问规则: "+rule.Name, nil); err != nil {
		return err
	}

	return nil
}

func (svc *Service) CreateIPLimitRule(ctx context.Context, siteID string, req *CreateIPLimitRuleRequest, requestID string) (*IPLimitRuleResponse, error) {
	if req.Name == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "名称不能为空", nil)
	}
	ruleType, err := normalizeIPRuleType(req.RuleType)
	if err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	normalizedIPs, err := NormalizeIPLimitEntries(req.IPs)
	if err != nil {
		return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
	}
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule := &repo.SiteIPWhitelistRule{
		ID:       NewIPLimitRuleID(),
		SiteID:   siteID,
		Name:     req.Name,
		RuleType: ruleType,
		IPsJSON:  mustJSONString(normalizedIPs),
		Enabled:  enabled,
	}
	if err := svc.ipWhitelistRuleRepo.Create(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.renderAndApply(ctx, site, requestID, "创建 IP 限制规则: "+req.Name, nil); err != nil {
		_ = svc.ipWhitelistRuleRepo.Delete(rule.ID)
		return nil, err
	}
	return ipLimitRuleToResponse(rule), nil
}

func (svc *Service) UpdateIPLimitRule(ctx context.Context, siteID, ruleID string, req *UpdateIPLimitRuleRequest, requestID string) (*IPLimitRuleResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	rule, err := svc.ipWhitelistRuleRepo.GetByID(ruleID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if rule == nil || rule.SiteID != siteID {
		return nil, app.NewAppError(app.ErrNotFound, "规则不存在", nil)
	}
	if req.Name != "" {
		rule.Name = req.Name
	}
	if req.RuleType != "" {
		normalizedType, err := normalizeIPRuleType(req.RuleType)
		if err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
		rule.RuleType = normalizedType
	}
	if len(req.IPs) > 0 {
		normalizedIPs, err := NormalizeIPLimitEntries(req.IPs)
		if err != nil {
			return nil, app.NewAppError(app.ErrValidationFailed, err.Error(), nil)
		}
		rule.IPsJSON = mustJSONString(normalizedIPs)
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if err := svc.ipWhitelistRuleRepo.Update(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.renderAndApply(ctx, site, requestID, "更新 IP 限制规则: "+rule.Name, nil); err != nil {
		return nil, err
	}
	return ipLimitRuleToResponse(rule), nil
}

func (svc *Service) DeleteIPLimitRule(ctx context.Context, siteID, ruleID string, requestID string) error {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	rule, err := svc.ipWhitelistRuleRepo.GetByID(ruleID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if rule == nil || rule.SiteID != siteID {
		return app.NewAppError(app.ErrNotFound, "规则不存在", nil)
	}
	if err := svc.ipWhitelistRuleRepo.Delete(ruleID); err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.renderAndApply(ctx, site, requestID, "删除 IP 限制规则: "+rule.Name, nil); err != nil {
		return err
	}
	return nil
}

func (svc *Service) ensureAccessLimitPath(site *repo.Site) {
	if site.AccessLimitPath == "" && site.PrimaryDomain != "" {
		site.AccessLimitPath = filepath.Join(svc.panelDir, "access-limit", site.PrimaryDomain+".conf")
	}
}

func (svc *Service) buildAccessLimitContent(siteID string) string {
	var buf strings.Builder
	if svc.ipWhitelistRuleRepo != nil {
		rules, _ := svc.ipWhitelistRuleRepo.ListBySiteID(siteID)
		var allowEntries []string
		var denyEntries []string
		for _, rule := range rules {
			if !rule.Enabled {
				continue
			}
			if strings.ToLower(rule.RuleType) == "deny" {
				denyEntries = append(denyEntries, repo.ParseJSONStringSlice(rule.IPsJSON)...)
			} else {
				allowEntries = append(allowEntries, repo.ParseJSONStringSlice(rule.IPsJSON)...)
			}
		}
		if len(denyEntries) > 0 {
			buf.WriteString(RenderIPBlacklistRule(denyEntries))
			buf.WriteString("\n")
		}
		if len(allowEntries) > 0 {
			buf.WriteString(RenderIPAllowRule(allowEntries))
			buf.WriteString("\n")
		}
	}

	authRules, _ := svc.authRuleRepo.ListBySiteID(siteID)
	if len(authRules) > 0 {
		buf.WriteString("# === 加密访问规则 ===\n")
		for _, rule := range authRules {
			if !rule.Enabled {
				continue
			}
			buf.WriteString(fmt.Sprintf("\n# %s\n", rule.Name))
			buf.WriteString(RenderAuthRule(rule.ID, rule.Path, rule.HtpasswdPath))
		}
	}

	denyRules, _ := svc.denyRuleRepo.ListBySiteID(siteID)
	if len(denyRules) > 0 {
		buf.WriteString("\n# === 禁止访问规则 ===\n")
		for _, rule := range denyRules {
			if !rule.Enabled {
				continue
			}
			buf.WriteString(fmt.Sprintf("\n# %s\n", rule.Name))
			if rule.ExtensionPattern != "" {
				rendered := RenderDenyExtensionRule(rule.ExtensionPattern)
				if rendered != "" {
					buf.WriteString(rendered)
				}
			}
			if rule.PathPattern != "" {
				buf.WriteString(RenderDenyPathRule(rule.PathPattern))
			}
		}
	}

	return buf.String()
}

func (svc *Service) renderAndApply(ctx context.Context, site *repo.Site, requestID, message string, extraFiles map[string]string) error {
	svc.ensureAccessLimitPath(site)

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.update_access_limit", TargetType: "site", TargetID: site.ID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("%s 站点 %s", message, site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	accessLimitContent := svc.buildAccessLimitContent(site.ID)

	changes := []agentclient.FileChangeRequest{
		{
			Type: "mkdir",
			Path: filepath.Dir(site.AccessLimitPath),
			Perm: 0755,
		},
		{
			Type:          "write",
			Path:          site.AccessLimitPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(accessLimitContent)),
			Perm:          0644,
		},
	}

	for path, content := range extraFiles {
		changes = append(changes, agentclient.FileChangeRequest{
			Type: "mkdir",
			Path: filepath.Dir(path),
			Perm: 0755,
		})
		changes = append(changes, agentclient.FileChangeRequest{
			Type:          "write",
			Path:          path,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)),
			Perm:          0644,
		})
	}

	svc.maybeInjectAccessLimitMarker(ctx, site, &changes)

	_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return app.NewAppError(app.ErrAgentUnavailable, "应用配置失败: "+agentErr.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")
	slog.Info("访问限制配置已更新", "site_id", site.ID, "operation_id", opID)
	return nil
}

func (svc *Service) maybeInjectAccessLimitMarker(ctx context.Context, site *repo.Site, changes *[]agentclient.FileChangeRequest) {
	if svc.configReader == nil || site.ConfigPath == "" {
		return
	}
	configContent, _, err := svc.configReader.ReadFile(ctx, site.ConfigPath)
	if err != nil || len(configContent) == 0 {
		return
	}
	markerBody := []byte("    include " + site.AccessLimitPath + ";\n")
	patched, injectErr := nginx.EnsureMarkerBlock(configContent, nginx.MarkerNameAccessLimit, markerBody)
	if injectErr != nil {
		slog.Warn("注入 ACCESS-LIMIT marker 失败", "site_id", site.ID, "error", injectErr)
		return
	}
	*changes = append(*changes, agentclient.FileChangeRequest{
		Type:          "write",
		Path:          site.ConfigPath,
		ContentBase64: base64.StdEncoding.EncodeToString(patched),
		Perm:          0644,
	})
}

func cleanExtensionPattern(raw string) string {
	parts := strings.Split(raw, ",")
	var cleaned []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, ".") {
			p = "." + p
		}
		cleaned = append(cleaned, p)
	}
	return strings.Join(cleaned, ", ")
}

func mustJSONString(values []string) string {
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func NewIPLimitRuleID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return "ipl_" + hex.EncodeToString(b)
}
