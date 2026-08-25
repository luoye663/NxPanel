package geoaccess

import "github.com/luoye663/nxpanel/internal/db/repo"

const (
	ActionAllow   = "allow"
	ActionRespond = "respond"
	ActionDeny403 = "deny_403"
	ActionDeny444 = "deny_444"
	ResponseHTML  = "html"
	ResponseText  = "text"
	maxRules      = 64
	maxBodyBytes  = 64 * 1024
)

type GeoIPSettingsResponse struct {
	AccountID        string   `json:"account_id"`
	LicenseKeyMasked string   `json:"license_key_masked"`
	AutoUpdate       bool     `json:"auto_update"`
	TrustedProxies   []string `json:"trusted_proxies"`
}

type UpdateGeoIPSettingsRequest struct {
	AccountID      string    `json:"account_id"`
	LicenseKey     *string   `json:"license_key,omitempty"`
	AutoUpdate     *bool     `json:"auto_update,omitempty"`
	TrustedProxies *[]string `json:"trusted_proxies,omitempty"`
}

type GeoIPStatusResponse struct {
	Installed     bool     `json:"installed"`
	Checksum      string   `json:"checksum"`
	BuildEpoch    int64    `json:"build_epoch"`
	Countries     []string `json:"countries"`
	LastAttemptAt string   `json:"last_attempt_at,omitempty"`
	LastSuccessAt string   `json:"last_success_at,omitempty"`
	LastError     string   `json:"last_error,omitempty"`
	EnabledSites  int      `json:"enabled_sites"`
}

type SiteAccessResponse struct {
	SiteID              string `json:"site_id"`
	Enabled             bool   `json:"enabled"`
	DefaultAction       string `json:"default_action"`
	DefaultStatusCode   int    `json:"default_status_code"`
	DefaultResponseType string `json:"default_response_type"`
	DefaultResponseBody string `json:"default_response_body"`
	DesiredHash         string `json:"desired_hash"`
	AppliedHash         string `json:"applied_hash"`
	ApplyStatus         string `json:"apply_status"`
	LastError           string `json:"last_error,omitempty"`
}

type UpdateSiteAccessRequest struct {
	DefaultAction       string `json:"default_action"`
	DefaultStatusCode   int    `json:"default_status_code"`
	DefaultResponseType string `json:"default_response_type"`
	DefaultResponseBody string `json:"default_response_body"`
}

type RuleResponse struct {
	ID        string   `json:"id"`
	SiteID    string   `json:"site_id"`
	Name      string   `json:"name"`
	Countries []string `json:"countries"`
	Action    string   `json:"action"`
	Enabled   bool     `json:"enabled"`
	SortOrder int      `json:"sort_order"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

type CreateRuleRequest struct {
	Name      string   `json:"name"`
	Countries []string `json:"countries"`
	Action    string   `json:"action"`
	Enabled   *bool    `json:"enabled,omitempty"`
}

type UpdateRuleRequest struct {
	Name      *string   `json:"name,omitempty"`
	Countries *[]string `json:"countries,omitempty"`
	Action    *string   `json:"action,omitempty"`
	Enabled   *bool     `json:"enabled,omitempty"`
}

type ReorderRulesRequest struct {
	RuleIDs []string `json:"rule_ids"`
}

func siteSettingsResponse(item *repo.SiteGeoSettings) *SiteAccessResponse {
	return &SiteAccessResponse{SiteID: item.SiteID, Enabled: item.Enabled, DefaultAction: item.DefaultAction,
		DefaultStatusCode: item.DefaultStatusCode, DefaultResponseType: item.DefaultResponseType,
		DefaultResponseBody: item.DefaultResponseBody, DesiredHash: item.DesiredHash,
		AppliedHash: item.AppliedHash, ApplyStatus: item.ApplyStatus, LastError: item.LastError}
}

func ruleResponse(item *repo.SiteGeoRule) *RuleResponse {
	return &RuleResponse{ID: item.ID, SiteID: item.SiteID, Name: item.Name,
		Countries: repo.ParseJSONStringSlice(item.CountriesJSON), Action: item.Action, Enabled: item.Enabled,
		SortOrder: item.SortOrder, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
