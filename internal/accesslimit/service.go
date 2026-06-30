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
	accountRepo         *repo.AuthAccountRepo
	authRuleRepo        *repo.AuthRuleRepo
	denyRuleRepo        *repo.DenyRuleRepo
	ipWhitelistRuleRepo *repo.IPWhitelistRuleRepo
	proxyRepo           *repo.ProxyRepo
	opRepo              opRecorder
	agent               agentTx
	configReader        configReader
	panelDir            string
}

func NewService(
	siteRepo siteGetter,
	accountRepo *repo.AuthAccountRepo,
	authRuleRepo *repo.AuthRuleRepo,
	denyRuleRepo *repo.DenyRuleRepo,
	ipWhitelistRuleRepo *repo.IPWhitelistRuleRepo,
	proxyRepo *repo.ProxyRepo,
	opRepo opRecorder,
	agent agentTx,
	cr configReader,
	panelDir string,
) *Service {
	return &Service{
		siteRepo:            siteRepo,
		accountRepo:         accountRepo,
		authRuleRepo:        authRuleRepo,
		denyRuleRepo:        denyRuleRepo,
		ipWhitelistRuleRepo: ipWhitelistRuleRepo,
		proxyRepo:           proxyRepo,
		opRepo:              opRepo,
		agent:               agent,
		configReader:        cr,
		panelDir:            panelDir,
	}
}

type AuthRuleResponse struct {
	ID         string                 `json:"id"`
	SiteID     string                 `json:"site_id"`
	Name       string                 `json:"name"`
	Path       string                 `json:"path"`
	Username   string                 `json:"username"`
	AccountIDs []string               `json:"account_ids"`
	Accounts   []*AuthAccountResponse `json:"accounts"`
	Enabled    bool                   `json:"enabled"`
	SortOrder  int                    `json:"sort_order"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
}

type AuthAccountResponse struct {
	ID        string `json:"id"`
	Scope     string `json:"scope"`
	SiteID    string `json:"site_id,omitempty"`
	Username  string `json:"username"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CreateAuthAccountRequest struct {
	Scope    string `json:"scope"`
	SiteID   string `json:"site_id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled"`
}

type UpdateAuthAccountRequest struct {
	Scope    string `json:"scope"`
	SiteID   string `json:"site_id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Enabled  *bool  `json:"enabled"`
}

type CreateAuthRuleRequest struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	AccountIDs []string `json:"account_ids"`
	Enabled    *bool    `json:"enabled"`
}

type UpdateAuthRuleRequest struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	AccountIDs []string `json:"account_ids"`
	Enabled    *bool    `json:"enabled"`
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

func authAccountToResponse(a *repo.AuthAccount) *AuthAccountResponse {
	return &AuthAccountResponse{
		ID:        a.ID,
		Scope:     a.Scope,
		SiteID:    a.SiteID,
		Username:  a.Username,
		Enabled:   a.Enabled,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

func authRuleToResponse(r *repo.SiteAuthRule, accounts []*repo.AuthAccount) *AuthRuleResponse {
	accountIDs := make([]string, 0, len(accounts))
	accountResponses := make([]*AuthAccountResponse, 0, len(accounts))
	usernames := make([]string, 0, len(accounts))
	for _, account := range accounts {
		accountIDs = append(accountIDs, account.ID)
		accountResponses = append(accountResponses, authAccountToResponse(account))
		usernames = append(usernames, account.Username)
	}
	return &AuthRuleResponse{
		ID:         r.ID,
		SiteID:     r.SiteID,
		Name:       r.Name,
		Path:       r.Path,
		Username:   strings.Join(usernames, ", "),
		AccountIDs: accountIDs,
		Accounts:   accountResponses,
		Enabled:    r.Enabled,
		SortOrder:  r.SortOrder,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
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

func (svc *Service) ListAuthAccounts(siteID string) ([]*AuthAccountResponse, error) {
	if siteID != "" {
		site, err := svc.siteRepo.GetByID(siteID)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if site == nil {
			return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
		}
	}
	accounts, err := svc.accountRepo.ListForSite(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	result := make([]*AuthAccountResponse, 0, len(accounts))
	for _, account := range accounts {
		result = append(result, authAccountToResponse(account))
	}
	return result, nil
}

func (svc *Service) CreateAuthAccount(ctx context.Context, siteID string, req *CreateAuthAccountRequest, requestID string) (*AuthAccountResponse, error) {
	_ = ctx
	_ = requestID
	account, err := svc.buildAccountFromRequest(siteID, "", req.Scope, req.SiteID, req.Username, req.Password, req.Enabled)
	if err != nil {
		return nil, err
	}
	account.ID = NewAccountID()
	if err := svc.accountRepo.Create(account); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return authAccountToResponse(account), nil
}

func (svc *Service) UpdateAuthAccount(ctx context.Context, siteID, accountID string, req *UpdateAuthAccountRequest, requestID string) (*AuthAccountResponse, error) {
	account, err := svc.accountRepo.GetByID(accountID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if account == nil {
		return nil, app.NewAppError(app.ErrNotFound, "账户不存在", nil)
	}
	if !accountVisibleForSite(account, siteID) {
		return nil, app.NewAppError(app.ErrNotFound, "账户不存在", nil)
	}

	scope := account.Scope
	if req.Scope != "" {
		scope = req.Scope
	}
	reqSiteID := account.SiteID
	if req.SiteID != "" || scope == "global" {
		reqSiteID = req.SiteID
	}
	username := account.Username
	if req.Username != "" {
		username = req.Username
	}
	passwordHash := account.PasswordHash
	if req.Password != "" {
		passwordHash = GenerateHtpasswdEntry(username, req.Password)
	} else if username != account.Username {
		passwordHash = username + ":" + strings.TrimPrefix(account.PasswordHash, account.Username+":")
	}
	enabled := account.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	updated, buildErr := svc.buildAccountFromRequest(siteID, accountID, scope, reqSiteID, username, "__skip_password__", &enabled)
	if buildErr != nil {
		return nil, buildErr
	}
	updated.ID = accountID
	updated.PasswordHash = passwordHash
	updated.CreatedAt = account.CreatedAt
	if !updated.Enabled {
		if err := svc.ensureAccountCanBeDisabled(account.ID); err != nil {
			return nil, err
		}
	}

	if err := svc.accountRepo.Update(updated); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.refreshAccountReferences(ctx, updated, requestID); err != nil {
		if rollbackErr := svc.accountRepo.Update(account); rollbackErr != nil {
			slog.Warn("回滚访问账户更新失败", "account_id", account.ID, "error", rollbackErr)
		} else if refreshErr := svc.refreshAccountReferences(ctx, account, requestID); refreshErr != nil {
			slog.Warn("回滚访问账户引用文件失败", "account_id", account.ID, "error", refreshErr)
		}
		return nil, err
	}
	return authAccountToResponse(updated), nil
}

func (svc *Service) DeleteAuthAccount(ctx context.Context, siteID, accountID string, requestID string) error {
	_ = ctx
	_ = requestID
	account, err := svc.accountRepo.GetByID(accountID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if account == nil || !accountVisibleForSite(account, siteID) {
		return app.NewAppError(app.ErrNotFound, "账户不存在", nil)
	}
	refs, err := svc.accountRepo.CountReferences(accountID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if refs > 0 {
		return app.NewAppError(app.ErrValidationFailed, "账户正在被访问限制或反向代理引用，请先解除引用", nil)
	}
	if err := svc.accountRepo.Delete(accountID); err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	return nil
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
		accounts, err := svc.accountsForAuthRule(r.ID)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		result = append(result, authRuleToResponse(r, accounts))
	}
	return result, nil
}

func (svc *Service) CreateAuthRule(ctx context.Context, siteID string, req *CreateAuthRuleRequest, requestID string) (*AuthRuleResponse, error) {
	if req.Name == "" || req.Path == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "名称和路径不能为空", nil)
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
	accounts, accountIDs, err := svc.validateSelectableAccounts(siteID, req.AccountIDs, true)
	if err != nil {
		return nil, err
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	ruleID := NewRuleID()
	path := SanitizePath(req.Path)
	htpasswdPath := authRuleHtpasswdPath(svc.panelDir, ruleID)

	rule := &repo.SiteAuthRule{
		ID:           ruleID,
		SiteID:       siteID,
		Name:         req.Name,
		Path:         path,
		Username:     accountSummary(accounts),
		PasswordHash: "",
		HtpasswdPath: htpasswdPath,
		Enabled:      enabled,
	}

	if err := svc.authRuleRepo.Create(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.authRuleRepo.SetAccountIDs(rule.ID, accountIDs); err != nil {
		_ = svc.authRuleRepo.Delete(ruleID)
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.renderAndApply(ctx, site, requestID, "创建加密访问规则: "+req.Name, map[string]string{
		htpasswdPath: RenderHtpasswdContent(accountPasswordEntries(accounts)),
	}); err != nil {
		_ = svc.authRuleRepo.Delete(ruleID)
		return nil, err
	}

	return authRuleToResponse(rule, accounts), nil
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

	oldRule := *rule
	oldAccountIDs, err := svc.authRuleRepo.GetAccountIDs(rule.ID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
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

	var accounts []*repo.AuthAccount
	var accountIDs []string
	if len(req.AccountIDs) > 0 {
		var err error
		accounts, accountIDs, err = svc.validateSelectableAccounts(siteID, req.AccountIDs, true)
		if err != nil {
			return nil, err
		}
	} else {
		var err error
		accounts, err = svc.accountsForAuthRule(rule.ID)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if len(accountPasswordEntries(accounts)) == 0 {
			return nil, app.NewAppError(app.ErrValidationFailed, "请选择至少一个可用账户", nil)
		}
		accountIDs = accountIDsFromAccounts(accounts)
	}
	if rule.HtpasswdPath == "" {
		rule.HtpasswdPath = authRuleHtpasswdPath(svc.panelDir, rule.ID)
	}
	rule.Username = accountSummary(accounts)
	rule.PasswordHash = ""

	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}

	if err := svc.authRuleRepo.Update(rule); err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if err := svc.authRuleRepo.SetAccountIDs(rule.ID, accountIDs); err != nil {
		_ = svc.authRuleRepo.Update(&oldRule)
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	if err := svc.renderAndApply(ctx, site, requestID, "更新加密访问规则: "+rule.Name, map[string]string{
		rule.HtpasswdPath: RenderHtpasswdContent(accountPasswordEntries(accounts)),
	}); err != nil {
		_ = svc.authRuleRepo.Update(&oldRule)
		_ = svc.authRuleRepo.SetAccountIDs(rule.ID, oldAccountIDs)
		return nil, err
	}

	return authRuleToResponse(rule, accounts), nil
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
			if rule.HtpasswdPath == "" {
				rule.HtpasswdPath = authRuleHtpasswdPath(svc.panelDir, rule.ID)
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
		if strings.TrimSpace(path) == "" {
			continue
		}
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

func authRuleHtpasswdPath(panelDir, ruleID string) string {
	return filepath.Join(panelDir, "htpasswd", "auth-rule", ruleID+".htpasswd")
}

func proxyHtpasswdPath(panelDir, proxyID string) string {
	return filepath.Join(panelDir, "htpasswd", "proxy", proxyID+".htpasswd")
}

func accountSummary(accounts []*repo.AuthAccount) string {
	usernames := make([]string, 0, len(accounts))
	for _, account := range accounts {
		usernames = append(usernames, account.Username)
	}
	return strings.Join(usernames, ", ")
}

func accountPasswordEntries(accounts []*repo.AuthAccount) []string {
	entries := make([]string, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		entries = append(entries, account.PasswordHash)
	}
	return entries
}

func accountIDsFromAccounts(accounts []*repo.AuthAccount) []string {
	ids := make([]string, 0, len(accounts))
	for _, account := range accounts {
		ids = append(ids, account.ID)
	}
	return ids
}

func accountVisibleForSite(account *repo.AuthAccount, siteID string) bool {
	return account.Scope == "global" || account.SiteID == siteID
}

func (svc *Service) buildAccountFromRequest(currentSiteID, excludeID, scope, siteID, username, password string, enabledPtr *bool) (*repo.AuthAccount, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "site"
	}
	if scope != "global" && scope != "site" {
		return nil, app.NewAppError(app.ErrValidationFailed, "账户类型无效", nil)
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "用户名不能为空", nil)
	}
	if containsNginxInjectionChars(username) || strings.ContainsAny(username, ": ") {
		return nil, app.NewAppError(app.ErrValidationFailed, "用户名包含非法字符", nil)
	}
	exists, err := svc.accountRepo.UsernameExists(username, excludeID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if exists {
		return nil, app.NewAppError(app.ErrValidationFailed, "用户名已存在", nil)
	}
	if scope == "global" {
		siteID = ""
	} else {
		if siteID == "" {
			siteID = currentSiteID
		}
		if siteID == "" {
			return nil, app.NewAppError(app.ErrValidationFailed, "站点账户必须关联站点", nil)
		}
	}
	if scope == "site" {
		site, err := svc.siteRepo.GetByID(siteID)
		if err != nil {
			return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if site == nil {
			return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
		}
	}
	enabled := true
	if enabledPtr != nil {
		enabled = *enabledPtr
	}
	if password == "" {
		return nil, app.NewAppError(app.ErrValidationFailed, "密码不能为空", nil)
	}
	passwordHash := ""
	if password != "__skip_password__" {
		passwordHash = GenerateHtpasswdEntry(username, password)
	}
	return &repo.AuthAccount{Scope: scope, SiteID: siteID, Username: username, PasswordHash: passwordHash, Enabled: enabled}, nil
}

func (svc *Service) validateSelectableAccounts(siteID string, accountIDs []string, requireEnabled bool) ([]*repo.AuthAccount, []string, error) {
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
		return nil, nil, app.NewAppError(app.ErrValidationFailed, "请选择至少一个账户", nil)
	}
	accounts, err := svc.accountRepo.ListByIDs(ids)
	if err != nil {
		return nil, nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if len(accounts) != len(ids) {
		return nil, nil, app.NewAppError(app.ErrValidationFailed, "包含不存在的账户", nil)
	}
	for _, account := range accounts {
		if !accountVisibleForSite(account, siteID) {
			return nil, nil, app.NewAppError(app.ErrValidationFailed, "包含不可用于当前站点的账户", nil)
		}
		if requireEnabled && !account.Enabled {
			return nil, nil, app.NewAppError(app.ErrValidationFailed, "不能选择已禁用账户", nil)
		}
	}
	return accounts, ids, nil
}

func (svc *Service) ensureAccountCanBeDisabled(accountID string) error {
	ruleIDs, err := svc.authRuleRepo.ListRuleIDsByAccountID(accountID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	for _, ruleID := range ruleIDs {
		accounts, err := svc.accountsForAuthRule(ruleID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		available := 0
		for _, account := range accounts {
			if account.ID != accountID && account.Enabled {
				available++
			}
		}
		if available == 0 {
			return app.NewAppError(app.ErrValidationFailed, "该账户被加密访问规则引用，禁用后会导致规则没有可用账户", nil)
		}
	}
	if svc.proxyRepo == nil {
		return nil
	}
	proxyIDs, err := svc.proxyRepo.ListProxyIDsByAccountID(accountID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	for _, proxyID := range proxyIDs {
		proxy, err := svc.proxyRepo.GetByID(proxyID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if proxy == nil || !proxy.AuthEnabled {
			continue
		}
		accounts, err := svc.accountsForProxy(proxyID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		available := 0
		for _, account := range accounts {
			if account.ID != accountID && account.Enabled {
				available++
			}
		}
		if available == 0 {
			return app.NewAppError(app.ErrValidationFailed, "该账户被反向代理访问限制引用，禁用后会导致反向代理没有可用账户", nil)
		}
	}
	return nil
}

func (svc *Service) accountsForAuthRule(ruleID string) ([]*repo.AuthAccount, error) {
	ids, err := svc.authRuleRepo.GetAccountIDs(ruleID)
	if err != nil {
		return nil, err
	}
	return svc.accountRepo.ListByIDs(ids)
}

func (svc *Service) accountsForProxy(proxyID string) ([]*repo.AuthAccount, error) {
	if svc.proxyRepo == nil {
		return nil, nil
	}
	ids, err := svc.proxyRepo.GetAccountIDs(proxyID)
	if err != nil {
		return nil, err
	}
	return svc.accountRepo.ListByIDs(ids)
}

func (svc *Service) refreshAccountReferences(ctx context.Context, account *repo.AuthAccount, requestID string) error {
	ruleIDs, err := svc.authRuleRepo.ListRuleIDsByAccountID(account.ID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	visitedSites := map[string]*repo.Site{}
	for _, ruleID := range ruleIDs {
		rule, err := svc.authRuleRepo.GetByID(ruleID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if rule == nil {
			continue
		}
		site, ok := visitedSites[rule.SiteID]
		if !ok {
			var err error
			site, err = svc.siteRepo.GetByID(rule.SiteID)
			if err != nil {
				return app.NewAppError(app.ErrInternalError, err.Error(), nil)
			}
			if site == nil {
				continue
			}
			visitedSites[rule.SiteID] = site
		}
		accounts, err := svc.accountsForAuthRule(rule.ID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if len(accountPasswordEntries(accounts)) == 0 {
			return app.NewAppError(app.ErrValidationFailed, "该操作会导致加密访问规则没有可用账户，请先调整引用关系", nil)
		}
		if rule.HtpasswdPath == "" {
			rule.HtpasswdPath = authRuleHtpasswdPath(svc.panelDir, rule.ID)
		}
		if err := svc.renderAndApply(ctx, site, requestID, "更新账户引用: "+account.Username, map[string]string{
			rule.HtpasswdPath: RenderHtpasswdContent(accountPasswordEntries(accounts)),
		}); err != nil {
			return err
		}
	}
	if svc.proxyRepo == nil {
		return nil
	}
	proxyIDs, err := svc.proxyRepo.ListProxyIDsByAccountID(account.ID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	for _, proxyID := range proxyIDs {
		proxy, err := svc.proxyRepo.GetByID(proxyID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if proxy == nil || !proxy.AuthEnabled {
			continue
		}
		accounts, err := svc.accountsForProxy(proxy.ID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if proxy.AuthHtpasswdPath == "" {
			proxy.AuthHtpasswdPath = proxyHtpasswdPath(svc.panelDir, proxy.ID)
			_ = svc.proxyRepo.Update(proxy)
		}
		if len(accountPasswordEntries(accounts)) == 0 {
			return app.NewAppError(app.ErrValidationFailed, "该操作会导致反向代理没有可用账户，请先调整引用关系", nil)
		}
		site, err := svc.siteRepo.GetByID(proxy.SiteID)
		if err != nil {
			return app.NewAppError(app.ErrInternalError, err.Error(), nil)
		}
		if site == nil {
			continue
		}
		opID := app.NewOperationID()
		_ = svc.opRepo.Create(&repo.Operation{ID: opID, Action: "site.update_proxy_auth", TargetType: "site", TargetID: site.ID, Status: "pending", RequestID: requestID, Actor: "admin", Message: "更新反向代理访问账户", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
		_, agentErr := svc.agent.ApplyTransaction(ctx, &agentclient.TransactionRequest{
			OperationID: opID,
			Changes: []agentclient.FileChangeRequest{
				{Type: "mkdir", Path: filepath.Dir(proxy.AuthHtpasswdPath), Perm: 0755},
				{Type: "write", Path: proxy.AuthHtpasswdPath, ContentBase64: base64.StdEncoding.EncodeToString([]byte(RenderHtpasswdContent(accountPasswordEntries(accounts)))), Perm: 0644},
			},
			TestNginx:   true,
			ReloadNginx: site.Status == "enabled",
		})
		if agentErr != nil {
			_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
			return app.NewAppError(app.ErrAgentUnavailable, "应用反代访问账户失败: "+agentErr.Error(), nil)
		}
		_ = svc.opRepo.UpdateStatus(opID, "success")
	}
	return nil
}

func NewIPLimitRuleID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return "ipl_" + hex.EncodeToString(b)
}
