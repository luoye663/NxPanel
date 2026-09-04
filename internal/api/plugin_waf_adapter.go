package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/plugin"
	"github.com/luoye663/nxpanel/internal/waf"
	"github.com/luoye663/nxpanel/internal/wafcontrol"
)

type wafPluginAdapter struct{ server *Server }

func (a *wafPluginAdapter) BeforeEnable(r *http.Request, item *plugin.Installation) error {
	if item.Manifest == nil || !contains(item.Manifest.Permissions, "native.waf.modsecurity") {
		return nil
	}
	if a.server == nil || a.server.agentClient == nil {
		return errors.New("WAF agent is unavailable")
	}
	runtimeInfo, err := a.server.agentClient.DetectWAFRuntime(r.Context())
	if err != nil {
		return err
	}
	if len(runtimeInfo.BuiltinCandidates) == 0 {
		return errors.New("no compatible built-in ModSecurity module is available for this Nginx runtime")
	}
	candidate := runtimeInfo.BuiltinCandidates[0]
	if _, err = a.server.agentClient.InstallWAFProvider(r.Context(), &agentclient.WAFProviderInstallRequest{
		ProviderID: waf.ProviderID, ABIVersion: waf.ABIVersion, Version: item.ActiveVersion,
		OperationID: app.NewID("wafop"), ArtifactPath: candidate.Path, ArtifactSHA256: candidate.SHA256,
		RuntimeFingerprint: runtimeInfo.Fingerprint,
	}); err != nil {
		return err
	}
	_, err = a.server.agentClient.ActivateWAFProvider(r.Context(), &agentclient.WAFProviderActivateRequest{
		ProviderID: waf.ProviderID, ABIVersion: waf.ABIVersion, Version: item.ActiveVersion,
		OperationID: app.NewID("wafop"), RuntimeFingerprint: runtimeInfo.Fingerprint,
	})
	return err
}

func (a *wafPluginAdapter) BeforeDisable(r *http.Request, item *plugin.Installation) error {
	if item.Manifest == nil || !contains(item.Manifest.Permissions, "native.waf.modsecurity") || a.server == nil || a.server.wafSvc == nil {
		return nil
	}
	active, err := a.server.wafSvc.HasEnabledSites(r.Context())
	if err != nil {
		return err
	}
	if active {
		return errors.New("disable WAF on every protected site before disabling the plugin")
	}
	return nil
}

func (a *wafPluginAdapter) Invoke(_ http.ResponseWriter, r *http.Request, item *plugin.Installation, req pluginRPCRequest) (bool, any, error) {
	switch req.Method {
	case "waf.overview", "waf.events.list", "waf.rules.check", "waf.site.get", "waf.site.save":
	default:
		return false, nil, nil
	}
	if item == nil || item.Manifest == nil || !contains(item.Manifest.Permissions, "native.waf.modsecurity") || a.server == nil || a.server.wafSvc == nil {
		return true, nil, errors.New("WAF provider is not available")
	}
	switch req.Method {
	case "waf.overview", "waf.rules.check":
		data, err := a.server.wafSvc.Overview(r.Context())
		return true, data, err
	case "waf.events.list":
		return true, map[string]any{"items": []any{}}, nil
	case "waf.site.get":
		policy, _, err := a.server.wafSvc.GetSite(r.Context(), rpcSiteID(req))
		return true, policy, err
	case "waf.site.save":
		var policy waf.SitePolicy
		if len(req.Payload) == 0 || json.Unmarshal(req.Payload, &policy) != nil {
			return true, nil, errors.New("invalid WAF site policy")
		}
		siteID := rpcSiteID(req)
		if policy.SiteID == "" {
			policy.SiteID = siteID
		}
		if policy.SiteID != siteID {
			return true, nil, errors.New("site context mismatch")
		}
		saved, err := a.server.wafSvc.SaveSite(r.Context(), policy, wafcontrol.DefaultRulesVersion, app.NewID("wafop"))
		return true, saved, err
	}
	return false, nil, nil
}

func rpcSiteID(req pluginRPCRequest) string {
	var value string
	if raw := req.Context["site_id"]; raw != nil {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}
