package wafcontrol

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/waf"
)

const DefaultRulesVersion = "4.25.0"

type Service struct {
	db    *sql.DB
	sites *repo.SiteRepo
	agent *agentclient.Client
}

func NewService(db *sql.DB, sites *repo.SiteRepo, agent *agentclient.Client) *Service {
	return &Service{db: db, sites: sites, agent: agent}
}

func (s *Service) GetSite(ctx context.Context, siteID string) (waf.SitePolicy, string, error) {
	if _, err := s.requireSite(ctx, siteID); err != nil {
		return waf.SitePolicy{}, "", err
	}
	p := waf.DefaultSitePolicy(siteID)
	p.Enabled = false
	var enabled int
	var exclusions string
	var rulesVersion, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT enabled, mode, paranoia_level, inbound_threshold,
		outbound_threshold, response_status, request_body_limit, request_body_no_files_limit,
		response_body_limit, exclusions_json, rules_version, updated_at
		FROM waf_site_policies WHERE site_id=?`, siteID).Scan(&enabled, &p.Mode, &p.ParanoiaLevel,
		&p.InboundThreshold, &p.OutboundThreshold, &p.ResponseStatus, &p.RequestBodyLimit,
		&p.NoFilesBodyLimit, &p.ResponseBodyLimit, &exclusions, &rulesVersion, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, DefaultRulesVersion, nil
	}
	if err != nil {
		return waf.SitePolicy{}, "", err
	}
	p.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(exclusions), &p.Exclusions); err != nil {
		return waf.SitePolicy{}, "", err
	}
	return p, rulesVersion, nil
}

func (s *Service) SaveSite(ctx context.Context, policy waf.SitePolicy, rulesVersion, operationID string) (waf.SitePolicy, error) {
	site, err := s.requireSite(ctx, policy.SiteID)
	if err != nil {
		return waf.SitePolicy{}, err
	}
	if rulesVersion == "" {
		rulesVersion = DefaultRulesVersion
	}
	if policy.NoFilesBodyLimit == 0 {
		policy.NoFilesBodyLimit = 128 * 1024
	}
	previous, previousRules, previousErr := s.GetSite(ctx, policy.SiteID)
	if previousErr != nil {
		return waf.SitePolicy{}, previousErr
	}
	policy.Audit.Enabled = true
	if _, err := s.agent.ApplyWAFSite(ctx, &agentclient.WAFSiteApplyRequest{
		ProviderID: waf.ProviderID, ABIVersion: waf.ABIVersion, OperationID: operationID,
		SiteConfigPath: site.ConfigPath, RulesVersion: rulesVersion, Policy: policy,
	}); err != nil {
		return waf.SitePolicy{}, err
	}
	exclusions, _ := json.Marshal(policy.Exclusions)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx, `INSERT INTO waf_site_policies
		(site_id, enabled, mode, paranoia_level, inbound_threshold, outbound_threshold,
		response_status, request_body_limit, request_body_no_files_limit, response_body_limit,
		exclusions_json, rules_version, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(site_id) DO UPDATE SET enabled=excluded.enabled, mode=excluded.mode,
		paranoia_level=excluded.paranoia_level, inbound_threshold=excluded.inbound_threshold,
		outbound_threshold=excluded.outbound_threshold, response_status=excluded.response_status,
		request_body_limit=excluded.request_body_limit, request_body_no_files_limit=excluded.request_body_no_files_limit,
		response_body_limit=excluded.response_body_limit, exclusions_json=excluded.exclusions_json,
		rules_version=excluded.rules_version, updated_at=excluded.updated_at`, policy.SiteID, policy.Enabled,
		policy.Mode, policy.ParanoiaLevel, policy.InboundThreshold, policy.OutboundThreshold,
		policy.ResponseStatus, policy.RequestBodyLimit, policy.NoFilesBodyLimit, policy.ResponseBodyLimit,
		string(exclusions), rulesVersion, now)
	if err != nil {
		// The data plane has already reloaded. Restore its prior policy when the
		// desired-state write fails so the database and actual traffic behavior
		// cannot silently diverge.
		previous.Audit.Enabled = true
		rollbackErr := func() error {
			rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			_, applyErr := s.agent.ApplyWAFSite(rollbackCtx, &agentclient.WAFSiteApplyRequest{
				ProviderID: waf.ProviderID, ABIVersion: waf.ABIVersion, OperationID: operationID + "-rollback",
				SiteConfigPath: site.ConfigPath, RulesVersion: previousRules, Policy: previous,
			})
			return applyErr
		}()
		if rollbackErr != nil {
			return waf.SitePolicy{}, fmt.Errorf("persist WAF policy after apply: %w; data-plane rollback also failed: %v", err, rollbackErr)
		}
		return waf.SitePolicy{}, fmt.Errorf("persist WAF policy after apply (data plane rolled back): %w", err)
	}
	return policy, nil
}

func (s *Service) Overview(ctx context.Context) (map[string]any, error) {
	runtimeInfo, err := s.agent.DetectWAFRuntime(ctx)
	if err != nil {
		return nil, err
	}
	var protected int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM waf_site_policies WHERE enabled=1`).Scan(&protected)
	return map[string]any{
		"installed":           len(runtimeInfo.BuiltinCandidates) > 0,
		"enabled":             protected > 0,
		"health":              map[bool]string{true: "healthy", false: "degraded"}[len(runtimeInfo.BuiltinCandidates) > 0],
		"runtime_fingerprint": runtimeInfo.Fingerprint.Digest,
		"rules_version":       DefaultRulesVersion, "rules_channel": "LTS", "protected_sites": protected,
		"events_24h": 0, "blocked_24h": 0, "observed_24h": 0,
	}, nil
}

func (s *Service) HasEnabledSites(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM waf_site_policies WHERE enabled=1`).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Service) Events(ctx context.Context, siteID string) ([]agentclient.WAFAuditItem, error) {
	if siteID == "" {
		return []agentclient.WAFAuditItem{}, nil
	}
	result, err := s.agent.ListWAFAuditEvents(ctx, &agentclient.WAFAuditListRequest{SiteID: siteID, Limit: 100})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (s *Service) requireSite(ctx context.Context, siteID string) (*repo.Site, error) {
	if err := waf.ValidateSiteID(siteID); err != nil {
		return nil, err
	}
	site, err := s.sites.GetByIDContext(ctx, siteID)
	if err != nil {
		return nil, err
	}
	if site == nil {
		return nil, errors.New("site not found")
	}
	return site, nil
}
