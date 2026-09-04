package agentclient

import (
	"time"

	"github.com/luoye663/nxpanel/internal/waf"
)

type WAFRuntimeDetectResponse struct {
	Fingerprint       waf.RuntimeFingerprint `json:"fingerprint"`
	BuiltinCandidates []WAFModuleCandidate   `json:"builtin_candidates"`
}

type WAFModuleCandidate struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type WAFProviderInstallRequest struct {
	ProviderID         string                 `json:"provider_id"`
	ABIVersion         int                    `json:"abi_version"`
	Version            string                 `json:"version"`
	OperationID        string                 `json:"operation_id"`
	ArtifactPath       string                 `json:"artifact_path"`
	ArtifactSHA256     string                 `json:"artifact_sha256"`
	RuntimeFingerprint waf.RuntimeFingerprint `json:"runtime_fingerprint"`
}

type WAFProviderInstallResponse struct {
	Installed    bool   `json:"installed"`
	Version      string `json:"version"`
	ModuleSHA256 string `json:"module_sha256"`
}

type WAFProviderActivateRequest struct {
	ProviderID         string                 `json:"provider_id"`
	ABIVersion         int                    `json:"abi_version"`
	Version            string                 `json:"version"`
	OperationID        string                 `json:"operation_id"`
	RuntimeFingerprint waf.RuntimeFingerprint `json:"runtime_fingerprint"`
}

type WAFProviderActivateResponse struct {
	Activated bool   `json:"activated"`
	Version   string `json:"version"`
}

type WAFSiteApplyRequest struct {
	ProviderID     string         `json:"provider_id"`
	ABIVersion     int            `json:"abi_version"`
	OperationID    string         `json:"operation_id"`
	SiteConfigPath string         `json:"site_config_path"`
	RulesVersion   string         `json:"rules_version"`
	Policy         waf.SitePolicy `json:"policy"`
}

type WAFSiteApplyResponse struct {
	Applied     bool   `json:"applied"`
	IncludePath string `json:"include_path"`
}

type WAFAuditListRequest struct {
	SiteID string `json:"site_id"`
	Limit  int    `json:"limit"`
}

type WAFAuditItem struct {
	EventID string    `json:"event_id"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type WAFAuditListResponse struct {
	Items []WAFAuditItem `json:"items"`
}

type WAFAuditReadRequest struct {
	SiteID   string `json:"site_id"`
	EventID  string `json:"event_id"`
	MaxBytes int64  `json:"max_bytes"`
}

type WAFAuditReadResponse struct {
	EventID   string `json:"event_id"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
}

type WAFAuditCleanupRequest struct {
	SiteID   string `json:"site_id"`
	MaxAge   string `json:"max_age"`
	MaxBytes int64  `json:"max_bytes"`
}

type WAFAuditCleanupResponse struct {
	Removed    int   `json:"removed"`
	FreedBytes int64 `json:"freed_bytes"`
}
