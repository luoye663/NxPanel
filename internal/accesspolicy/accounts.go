package accesspolicy

import (
	"fmt"
	"strings"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// Validate every reference, including inactive rules, before serializing any
// snapshot. An inactive rule must not smuggle another site's password into a
// backup or become invalid merely by switching its enabled flag later.
func (s *Service) validateAccountReferences(siteID string, p Policy) error {
	actions := []Action{p.DefaultAction}
	for _, rule := range p.Rules {
		actions = append(actions, rule.Action)
	}
	ids := map[string]bool{}
	for _, action := range actions {
		if action.Type != "auth" && len(action.AccountIDs) > 0 {
			return app.ErrValidationFailedMsg("只有密码验证动作可以引用访问账户", nil)
		}
		seen := map[string]bool{}
		for _, id := range action.AccountIDs {
			if seen[id] {
				return app.ErrValidationFailedMsg("密码规则不能重复引用同一账户", nil)
			}
			seen[id] = true
			ids[id] = true
		}
	}
	if len(ids) > MaxConditionValues {
		return app.ErrValidationFailedMsg("访问策略引用账户过多", nil)
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	for start := 0; start < len(list); start += 500 {
		end := start + 500
		if end > len(list) {
			end = len(list)
		}
		accounts, err := s.accounts.ListByIDs(list[start:end])
		if err != nil {
			return err
		}
		if len(accounts) != end-start {
			return app.ErrValidationFailedMsg("引用的访问账户不存在", nil)
		}
		for _, account := range accounts {
			if account.Scope != "global" && account.SiteID != siteID {
				return app.ErrValidationFailedMsg("访问账户不属于当前站点", nil)
			}
		}
	}
	return nil
}

// CheckAccountChange considers both live rules and pending edits. Deleting a
// referenced account or changing its scope cannot leave an unresolvable rule.
func (s *Service) CheckAccountChange(id string, replacement *repo.AuthAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.store.List()
	if err != nil {
		return err
	}
	for _, item := range items {
		policies := []Policy{item.Policy}
		applied, err := s.store.GetApplied(item.SiteID)
		if err != nil {
			return err
		}
		if applied != nil {
			policies = append(policies, *applied)
		}
		for _, p := range policies {
			for _, rule := range append(append([]Rule{}, p.Rules...), Rule{ID: "default", Name: "默认动作", Enabled: true, Action: p.DefaultAction}) {
				if rule.Action.Type != "auth" {
					continue
				}
				refs := false
				for _, ref := range rule.Action.AccountIDs {
					refs = refs || ref == id
				}
				if !refs {
					continue
				}
				if replacement == nil {
					return app.ErrValidationFailedMsg("账户正被统一访问策略引用，请先解除引用并应用策略", nil)
				}
				if replacement.Scope != "global" && replacement.SiteID != item.SiteID {
					return app.ErrValidationFailedMsg("修改账户作用域会使访问策略引用失效", nil)
				}
				if !rule.Enabled {
					continue
				}
				accounts, err := s.accounts.ListByIDs(rule.Action.AccountIDs)
				if err != nil {
					return err
				}
				enabled := 0
				seen := map[string]bool{}
				for _, account := range accounts {
					if account.ID == id {
						account = replacement
					}
					if !account.Enabled {
						continue
					}
					if seen[account.Username] {
						return app.ErrValidationFailedMsg("修改账户会导致密码规则存在重名账户", nil)
					}
					seen[account.Username] = true
					if strings.TrimSpace(account.PasswordHash) != "" {
						enabled++
					}
				}
				if enabled == 0 {
					return app.ErrValidationFailedMsg(fmt.Sprintf("规则 %s 至少需要一个启用账户", rule.Name), nil)
				}
			}
		}
	}
	return nil
}
