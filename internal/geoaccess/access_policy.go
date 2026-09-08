package geoaccess

import (
	"context"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

// Hooks keep legacy include generation away from sites managed by the unified
// compiler. Refresh runs under mutationMu; the provider below must not acquire it.
func (s *Service) SetAccessPolicyHooks(managed func(string) bool, refresh func(context.Context, string) error) {
	s.policyManaged = managed
	s.policyRefresh = refresh
}

func (s *Service) AccessPolicyData() (map[string][]string, []string, error) {
	settings, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return nil, nil, err
	}
	proxies, err := normalizeTrustedProxies(repo.ParseJSONStringSlice(settings.TrustedProxiesJSON))
	if err != nil {
		return nil, nil, err
	}
	if settings.ActiveCachePath == "" {
		return nil, proxies, nil
	}
	cache, err := readCache(settings.ActiveCachePath)
	if err != nil {
		return nil, nil, err
	}
	return cache.Networks, proxies, nil
}

func (s *Service) refreshAccessPolicies(ctx context.Context, requestID string) error {
	if s.policyRefresh != nil {
		return s.policyRefresh(ctx, requestID)
	}
	return nil
}
