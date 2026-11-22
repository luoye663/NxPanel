package geoaccess

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/oschwald/maxminddb-golang"
)

// Hooks keep legacy include generation away from sites managed by the unified
// compiler. Refresh runs under mutationMu; the provider below must not acquire it.
func (s *Service) SetAccessPolicyHooks(managed func(string) bool, refresh func(context.Context, string) error) {
	s.policyManaged = managed
	s.policyRefresh = refresh
}

func (s *Service) AccessPolicyData() (bool, []string, error) {
	settings, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return false, nil, err
	}
	proxies, err := normalizeTrustedProxies(repo.ParseJSONStringSlice(settings.TrustedProxiesJSON))
	if err != nil {
		return false, nil, err
	}
	return settings.ActiveDBPath != "" && settings.Checksum != "", proxies, nil
}

func (s *Service) LookupCountry(ip netip.Addr) (string, error) {
	settings, err := s.repo.GetGeoIPSettings()
	if err != nil {
		return "", err
	}
	database, err := maxminddb.Open(settings.ActiveDBPath)
	if err != nil {
		return "", fmt.Errorf("读取 GeoIP 数据库: %w", err)
	}
	defer database.Close()
	var record mmdbCountryRecord
	if err := database.Lookup(net.IP(ip.Unmap().AsSlice()), &record); err != nil {
		return "", fmt.Errorf("查询 GeoIP 数据库: %w", err)
	}
	return record.countryCode(), nil
}

func (record mmdbCountryRecord) countryCode() string {
	for _, raw := range []string{record.Country.ISOCode, record.RegisteredCountry.ISOCode} {
		code := strings.ToUpper(raw)
		if len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
			return code
		}
	}
	return "ZZ"
}

func (s *Service) refreshAccessPolicies(ctx context.Context, requestID string) error {
	if s.policyRefresh != nil {
		return s.policyRefresh(ctx, requestID)
	}
	return nil
}
