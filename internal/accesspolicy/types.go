// Package accesspolicy compiles ordered site access rules to native Nginx directives.
package accesspolicy

const (
	MaxRules           = 256
	MaxConditions      = 16
	MaxConditionValues = 65536
	MaxRenderedBytes   = 64 * 1024 * 1024
)

type Policy struct {
	PendingSource string   `json:"pending_source,omitempty"`
	Version       int64    `json:"version"`
	Mode          string   `json:"mode"`
	Rules         []Rule   `json:"rules"`
	DefaultAction Action   `json:"default_action"`
	ApplyStatus   string   `json:"apply_status"`
	LastError     string   `json:"last_error"`
	Warnings      []string `json:"warnings"`
}
type Rule struct {
	SourceDisabled bool        `json:"source_disabled,omitempty"`
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	Enabled        bool        `json:"enabled"`
	Match          string      `json:"match"`
	Conditions     []Condition `json:"conditions"`
	Action         Action      `json:"action"`
	SourceType     string      `json:"source_type,omitempty"`
	SourceID       string      `json:"source_id,omitempty"`
}
type Condition struct {
	Kind     string   `json:"kind"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
	Negate   bool     `json:"negate"`
}
type Action struct {
	Type         string   `json:"type"`
	StatusCode   int      `json:"status_code,omitempty"`
	ResponseType string   `json:"response_type,omitempty"`
	ResponseBody string   `json:"response_body,omitempty"`
	AccountIDs   []string `json:"account_ids,omitempty"`
}
type RenderContext struct {
	CountryNetworks map[string][]string
	ServerNames     []string
	// AuthFiles contains service-resolved htpasswd content, keyed by rule ID or "default".
	AuthFiles map[string]string
}
type CompiledPolicy struct {
	GlobalContent string
	ServerContent string
	Files         map[string]string
}
type PreviewRequest struct {
	IP      string `json:"ip"`
	Country string `json:"country,omitempty"`
	Path    string `json:"path"`
	Referer string `json:"referer,omitempty"`
}
type RulePreview struct {
	ID         string `json:"id"`
	Matched    bool   `json:"matched"`
	Conditions []bool `json:"conditions"`
	Evaluated  bool   `json:"evaluated"`
}
type PreviewResult struct {
	Rules                  []RulePreview `json:"rules"`
	MatchedRuleID          string        `json:"matched_rule_id"`
	Action                 Action        `json:"action"`
	RequiresAuthentication bool          `json:"requires_authentication"`
}
