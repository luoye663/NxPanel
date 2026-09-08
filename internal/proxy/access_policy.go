package proxy

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

// AccessPolicyPrepare augments a proxy transaction with the matching access
// policy. The completion callback receives the Agent outcome before audit writes.
// Hooks run under writeMu; they must not call another proxy write method.
type AccessPolicyPrepare func(context.Context, *repo.Site, []*repo.SiteProxy, []agentclient.FileChangeRequest) ([]agentclient.FileChangeRequest, func(error) error, error)

func (svc *Service) SetAccessPolicyHooks(managed func(string) bool, prepare AccessPolicyPrepare, guards ...func(string, bool) error) {
	svc.accessPolicyManaged, svc.accessPolicyPrepare = managed, prepare
	if len(guards) > 0 {
		svc.accessPolicyGuard = guards[0]
	}
}

func (svc *Service) guardAccessPolicyWrite(siteID string, sync bool) error {
	if svc.accessPolicyGuard != nil {
		return svc.accessPolicyGuard(siteID, sync)
	}
	return nil
}

func (svc *Service) accessPolicyOwnsAuth(siteID string) bool {
	return svc.accessPolicyManaged != nil && svc.accessPolicyManaged(siteID)
}

func (svc *Service) rejectLegacyAuth(siteID string, enabled bool, accountIDs []string) error {
	if svc.accessPolicyOwnsAuth(siteID) && (enabled || len(accountIDs) > 0) {
		return app.NewAppError(app.ErrValidationFailed, "该站点已使用统一访问策略，请在访问限制中设置反代密码规则", nil)
	}
	return nil
}

// WithAccessPolicyConfig serializes activation with proxy updates and removes
// only the exact authentication directive pairs generated for existing proxies.
// The callback owns the single transaction applying the resulting configuration.
func (svc *Service) WithAccessPolicyConfig(ctx context.Context, siteID string, apply func([]byte) error) error {
	svc.writeMu.Lock()
	defer svc.writeMu.Unlock()
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return err
	}
	if site == nil {
		return app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	content, _, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return err
	}
	proxies, err := svc.proxyRepo.ListBySiteID(siteID)
	if err != nil {
		return err
	}
	content, err = stripGeneratedProxyAuth(content, proxies, svc.panelDir)
	if err != nil {
		return err
	}
	return apply(content)
}

func stripGeneratedProxyAuth(content []byte, proxies []*repo.SiteProxy, panelDir string) ([]byte, error) {
	knownFiles := map[string]bool{}
	for _, proxy := range proxies {
		path := proxy.AuthHtpasswdPath
		if path == "" {
			path = proxyHtpasswdPath(panelDir, proxy.ID)
		}
		knownFiles["auth_basic_user_file "+path+";"] = true
	}
	for _, name := range []string{nginx.MarkerNameMainLocation, nginx.MarkerNameExtraLocations} {
		if !bytes.Contains(content, []byte(nginx.MarkerPrefix+name+"-START")) && !bytes.Contains(content, []byte(nginx.MarkerPrefix+name+"-END")) {
			continue
		}
		body, err := nginx.ExtractMarkerBlock(content, name)
		if err != nil {
			return nil, fmt.Errorf("读取反代配置标记失败: %w", err)
		}
		lines := strings.SplitAfter(string(body), "\n")
		var cleaned strings.Builder
		changed := false
		for i := 0; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == `auth_basic "Restricted";` && i+1 < len(lines) && knownFiles[strings.TrimSpace(lines[i+1])] {
				i++
				changed = true
				continue
			}
			cleaned.WriteString(lines[i])
		}
		if changed {
			content, err = nginx.ReplaceMarkerBlock(content, name, []byte(cleaned.String()))
			if err != nil {
				return nil, err
			}
		}
	}
	return content, nil
}
