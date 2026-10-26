package accesslimit

import (
	"context"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func (svc *Service) SetAccessPolicyHooks(managed func(string) bool, check func(string, *repo.AuthAccount) error, refresh func(context.Context, string) error) {
	svc.policyManaged = managed
	svc.policyAccountCheck = check
	svc.policyRefresh = refresh
}

func (svc *Service) legacyAccountReferences(accountID string) (int, error) {
	if svc.policyManaged == nil {
		return svc.accountRepo.CountReferences(accountID)
	}
	count := 0
	ids, err := svc.authRuleRepo.ListRuleIDsByAccountID(accountID)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		rule, err := svc.authRuleRepo.GetByID(id)
		if err != nil {
			return 0, err
		}
		if rule != nil && !svc.policyManaged(rule.SiteID) {
			count++
		}
	}
	if svc.proxyRepo != nil {
		ids, err = svc.proxyRepo.ListProxyIDsByAccountID(accountID)
		if err != nil {
			return 0, err
		}
		for _, id := range ids {
			p, err := svc.proxyRepo.GetByID(id)
			if err != nil {
				return 0, err
			}
			if p != nil && !svc.policyManaged(p.SiteID) {
				count++
			}
		}
	}
	return count, nil
}
