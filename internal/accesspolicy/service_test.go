package accesspolicy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	proxyservice "github.com/luoye663/nxpanel/internal/proxy"
)

type policyServiceAgent struct {
	files        map[string][]byte
	transactions []*agentclient.TransactionRequest
	applyErr     error
	nilResult    bool
}

func (a *policyServiceAgent) ReadFile(_ context.Context, path string) ([]byte, string, error) {
	content, ok := a.files[path]
	if !ok {
		return nil, "", errors.New("file not found: " + path)
	}
	return append([]byte(nil), content...), "", nil
}
func (a *policyServiceAgent) ApplyTransaction(_ context.Context, request *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	a.transactions = append(a.transactions, request)
	if a.applyErr != nil {
		return nil, a.applyErr
	}
	if a.nilResult {
		return nil, nil
	}
	for _, change := range request.Changes {
		if change.Type == "write" {
			content, err := base64.StdEncoding.DecodeString(change.ContentBase64)
			if err != nil {
				return nil, err
			}
			a.files[change.Path] = content
		} else if change.Type == "remove" {
			delete(a.files, change.Path)
		}
	}
	return &agentclient.TransactionResponse{}, nil
}
func (a *policyServiceAgent) FilesRemove(_ context.Context, paths []string) error {
	for _, path := range paths {
		delete(a.files, path)
	}
	return nil
}
func (*policyServiceAgent) FilesChown(context.Context, string, string, string, bool) error {
	return nil
}
func (*policyServiceAgent) FilesChmod(context.Context, string, string, bool) error { return nil }

const servicePolicyConfig = `server {
    #NXPANEL-ACCESS-LIMIT-START
    include /tmp/old-access.conf;
    #NXPANEL-ACCESS-LIMIT-END
    #NXPANEL-HOTLINK-START
    include /tmp/old-hotlink.conf;
    #NXPANEL-HOTLINK-END
    #NXPANEL-MAIN-LOCATION-START
    location / {
        auth_basic "Restricted";
        auth_basic_user_file /panel/auth/legacy.htpasswd;
        proxy_pass http://backend;
        add_header X-Custom "preserve";
    }
    #NXPANEL-MAIN-LOCATION-END
    #NXPANEL-EXTRA-LOCATIONS-START
    #NXPANEL-EXTRA-LOCATIONS-END
    add_header X-Server "also preserve";
}
`

func policyServiceFixture(t *testing.T) (*Service, *policyServiceAgent, *proxyservice.Service, *sql.DB) {
	t.Helper()
	database := policyTestDB(t)
	if err := nginx.InitTemplates("../../configs/templates"); err != nil {
		t.Fatal(err)
	}
	agent := &policyServiceAgent{files: map[string][]byte{"/tmp/site.conf": []byte(servicePolicyConfig)}}
	proxyRepo := repo.NewProxyRepo(database)
	if err := proxyRepo.Create(&repo.SiteProxy{ID: "legacy", SiteID: "site_policy", Name: "backend", Enabled: true, LocationPath: "/", UpstreamURL: "http://backend", AuthEnabled: true, AuthHtpasswdPath: "/panel/auth/legacy.htpasswd"}); err != nil {
		t.Fatal(err)
	}
	proxy := proxyservice.NewService(repo.NewSiteRepo(database), proxyRepo, repo.NewUpstreamRepo(database), repo.NewAuthAccountRepo(database), repo.NewOperationRepo(database), repo.NewBackupRepo(database), agent, &app.Config{Nginx: app.NginxConfig{PanelDir: "/panel"}})
	service := NewService(database, agent, nil, proxy, "/panel")
	proxy.SetAccessPolicyHooks(service.Managed, service.PrepareProxyChange, service.ProxyWriteGuard)
	return service, agent, proxy, database
}

func serviceAllowPolicy() Policy {
	return Policy{Mode: "unified", Rules: []Rule{}, DefaultAction: Action{Type: "allow"}}
}

func TestServiceSavePreservesCustomConfigurationInSingleTransaction(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	p, err := service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "request")
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != 1 || p.Mode != "unified" || p.ApplyStatus != "applied" || len(agent.transactions) != 1 {
		t.Fatalf("unexpected saved state: %#v transactions=%d", p, len(agent.transactions))
	}
	main := string(agent.files["/tmp/site.conf"])
	for _, want := range []string{`proxy_pass http://backend;`, `add_header X-Custom "preserve";`, `add_header X-Server "also preserve";`} {
		if !strings.Contains(main, want) {
			t.Fatalf("custom/handler content lost %s:\n%s", want, main)
		}
	}
	if strings.Contains(main, "auth_basic") || strings.Contains(main, "old-hotlink.conf") || strings.Contains(main, "old-access.conf") {
		t.Fatalf("old independent checks retained:\n%s", main)
	}
	if !agent.transactions[0].TestNginx || !agent.transactions[0].ReloadNginx {
		t.Fatal("policy must validate and reload once")
	}
	before := len(agent.transactions)
	_, err = service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "stale")
	var appErr *app.AppError
	if !errors.As(err, &appErr) || appErr.Code != app.ErrConflict || len(agent.transactions) != before {
		t.Fatalf("stale editor must conflict before file apply: %v", err)
	}
}

func TestServiceFailedActivationRetainsDesiredAndBlocksLegacy(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	agent.applyErr = errors.New("socket timeout")
	desired := serviceAllowPolicy()
	desired.DefaultAction = Action{Type: "deny", StatusCode: 451, ResponseBody: "pending"}
	if _, err := service.Save(context.Background(), "site_policy", desired, "request"); err == nil {
		t.Fatal("expected apply failure")
	}
	stored, err := service.Get("site_policy")
	if err != nil || stored.Mode != "legacy" || stored.Version != 1 || stored.DefaultAction.ResponseBody != "pending" {
		t.Fatalf("uncertain desired state lost: %#v, %v", stored, err)
	}
	if service.Managed("site_policy") {
		t.Fatal("uncertain first activation must remain legacy")
	}
	if err := service.GuardLegacy("site_policy"); err == nil {
		t.Fatal("stale old writer can overwrite potentially applied config")
	}
	if string(agent.files["/tmp/site.conf"]) != servicePolicyConfig {
		t.Fatal("failed transaction changed config")
	}
	agent.applyErr = nil
	applied, err := service.Sync(context.Background(), "site_policy", "sync")
	if err != nil || applied.Mode != "unified" || applied.Version != 1 || applied.ApplyStatus != "applied" {
		t.Fatalf("sync failed: %#v, %v", applied, err)
	}
	agent.applyErr = errors.New("nginx -t failed")
	desired.Version = 1
	desired.DefaultAction.ResponseBody = "second draft"
	if _, err := service.Save(context.Background(), "site_policy", desired, "next"); err == nil {
		t.Fatal("expected new draft failure")
	}
	snapshot, err := service.store.GetApplied("site_policy")
	if err != nil || snapshot.Version != 1 || snapshot.DefaultAction.ResponseBody != "pending" {
		t.Fatalf("applied snapshot overwritten by draft: %#v, %v", snapshot, err)
	}
}

func TestServiceRefreshOnlyUsesAppliedSnapshot(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	p := serviceAllowPolicy()
	p.DefaultAction = Action{Type: "deny", StatusCode: 451, ResponseBody: "published"}
	if _, err := service.Save(context.Background(), "site_policy", p, "first"); err != nil {
		t.Fatal(err)
	}
	p.Version = 1
	p.DefaultAction.ResponseBody = "unpublished"
	if _, err := service.store.SaveDesired("site_policy", p, 1); err != nil {
		t.Fatal(err)
	}
	if err := service.RefreshActive(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	mainPolicy := string(agent.files["/panel/access-limit/policy.example.com.conf"])
	if !strings.Contains(mainPolicy, "published") || strings.Contains(mainPolicy, "unpublished") {
		t.Fatalf("dependency refresh published draft:\n%s", mainPolicy)
	}
	stored, err := service.store.Get("site_policy")
	if err != nil || stored.Version != 2 || stored.ApplyStatus != "pending" || stored.DefaultAction.ResponseBody != "unpublished" {
		t.Fatalf("refresh modified draft metadata: %#v, %v", stored, err)
	}
}

func TestServiceRejectsCustomAuthenticationBeforeSaving(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	agent.files["/tmp/site.conf"] = []byte(strings.Replace(servicePolicyConfig, "    add_header X-Server", "    auth_basic \"custom\";\n    add_header X-Server", 1))
	if _, err := service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "request"); err == nil {
		t.Fatal("custom auth conflict accepted")
	}
	if len(agent.transactions) != 0 {
		t.Fatal("conflicting config reached Agent")
	}
	if p, err := service.store.Get("site_policy"); err != nil || p != nil {
		t.Fatalf("preflight saved policy: %#v, %v", p, err)
	}
}

func TestServiceAuthenticationReferencesAreScopedAndUnique(t *testing.T) {
	service, _, _, database := policyServiceFixture(t)
	if _, err := database.Exec(`INSERT INTO sites (id,primary_domain,domains_json,status,root_path,access_log_path,error_log_path,config_path,enabled_path,rewrite_path) VALUES ('other','other.example.com','[]','enabled','/tmp','/tmp/a','/tmp/e','/tmp/c','/tmp/n','/tmp/r')`); err != nil {
		t.Fatal(err)
	}
	for _, a := range []*repo.AuthAccount{
		{ID: "global", Scope: "global", Username: "global", PasswordHash: "global:{SHA}abc", Enabled: true},
		{ID: "local", Scope: "site", SiteID: "site_policy", Username: "local", PasswordHash: "local:{SHA}abc", Enabled: true},
		{ID: "foreign", Scope: "site", SiteID: "other", Username: "foreign", PasswordHash: "foreign:{SHA}abc", Enabled: true},
		{ID: "disabled", Scope: "site", SiteID: "site_policy", Username: "disabled", PasswordHash: "disabled:{SHA}abc", Enabled: false},
	} {
		if err := service.accounts.Create(a); err != nil {
			t.Fatal(err)
		}
	}
	if body, err := service.authBody("site_policy", []string{"local", "global"}); err != nil || body != "global:{SHA}abc\nlocal:{SHA}abc\n" {
		t.Fatalf("valid scoped accounts: %q, %v", body, err)
	}
	for _, ids := range [][]string{{"foreign"}, {"missing"}, {"global", "global"}, {"disabled"}} {
		if _, err := service.authBody("site_policy", ids); err == nil {
			t.Fatalf("invalid account IDs accepted: %#v", ids)
		}
	}
}

func TestServiceNilAgentResponseDoesNotConfirmActivation(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	agent.nilResult = true
	if _, err := service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "request"); err == nil {
		t.Fatal("nil transaction response confirmed activation")
	}
	if service.Managed("site_policy") {
		t.Fatal("nil transaction response changed live mode")
	}
}

func linkedProxyPolicy() Policy {
	p := serviceAllowPolicy()
	p.Rules = []Rule{{ID: "linked", Name: "linked", Enabled: true, Match: "all",
		Conditions: []Condition{{Kind: "path", Operator: "prefix", Values: []string{"/"}}},
		Action:     Action{Type: "deny", StatusCode: 403}, SourceType: "proxy", SourceID: "legacy"}}
	return p
}

func proxyPolicyUpdate(enabled bool, path string) *proxyservice.UpdateProxyRequest {
	return &proxyservice.UpdateProxyRequest{Name: "backend", Enabled: enabled, LocationPath: path, UpstreamURL: "http://backend:8080"}
}

func TestServiceLinkedProxyToggleKeepsUserRulePreference(t *testing.T) {
	service, _, proxy, _ := policyServiceFixture(t)
	if _, err := service.Save(context.Background(), "site_policy", linkedProxyPolicy(), "first"); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		if _, _, err := proxy.Update(context.Background(), "site_policy", "legacy", proxyPolicyUpdate(enabled, "/api"), "toggle"); err != nil {
			t.Fatal(err)
		}
		p, err := service.Get("site_policy")
		if err != nil || !p.Rules[0].Enabled || p.Rules[0].SourceDisabled == enabled || p.Rules[0].Conditions[0].Values[0] != "/api" {
			t.Fatalf("linked rule preference lost: %#v, %v", p, err)
		}
	}
	p, err := service.Get("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	p.Rules[0].Enabled = false
	if _, err := service.Save(context.Background(), "site_policy", *p, "manual-disable"); err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{false, true} {
		if _, _, err := proxy.Update(context.Background(), "site_policy", "legacy", proxyPolicyUpdate(enabled, "/api"), "toggle"); err != nil {
			t.Fatal(err)
		}
	}
	p, err = service.Get("site_policy")
	if err != nil || p.Rules[0].Enabled || p.Rules[0].SourceDisabled {
		t.Fatalf("proxy re-enable restored manually disabled rule: %#v, %v", p, err)
	}
}

func TestServiceProxyRetryAppliesMatchingRouteAndPolicyTogether(t *testing.T) {
	service, agent, proxy, _ := policyServiceFixture(t)
	if _, err := service.Save(context.Background(), "site_policy", linkedProxyPolicy(), "first"); err != nil {
		t.Fatal(err)
	}
	agent.applyErr = errors.New("nginx reload failed")
	if _, _, err := proxy.Update(context.Background(), "site_policy", "legacy", proxyPolicyUpdate(true, "/new-api"), "update"); err == nil {
		t.Fatal("expected proxy apply failure")
	}
	p, err := service.Get("site_policy")
	if err != nil || p.PendingSource != "proxy" || p.ApplyStatus == "applied" {
		t.Fatalf("proxy pending tag lost: %#v, %v", p, err)
	}
	version, attempts := p.Version, len(agent.transactions)
	if _, err := service.Save(context.Background(), "site_policy", *p, "blocked-policy-edit"); err == nil {
		t.Fatal("ordinary policy save accepted pending proxy transaction")
	}
	p, err = service.Get("site_policy")
	if err != nil || p.Version != version || len(agent.transactions) != attempts {
		t.Fatalf("rejected policy save mutated pending proxy transaction: %#v, %v", p, err)
	}
	if strings.Contains(string(agent.files["/tmp/site.conf"]), "location /new-api") {
		t.Fatal("failed proxy transaction changed route")
	}
	agent.applyErr = nil
	before := len(agent.transactions)
	if _, err := service.Sync(context.Background(), "site_policy", "retry"); err != nil {
		t.Fatal(err)
	}
	if len(agent.transactions) != before+1 || !strings.Contains(string(agent.files["/tmp/site.conf"]), "location /new-api") {
		t.Fatal("retry did not atomically apply updated proxy route")
	}
	p, err = service.Get("site_policy")
	if err != nil || p.ApplyStatus != "applied" || p.Rules[0].Conditions[0].Values[0] != "/new-api" {
		t.Fatalf("retry policy route mismatch: %#v, %v", p, err)
	}
}

func TestServicePendingPolicyBlocksProxyBeforeDatabaseMutation(t *testing.T) {
	service, agent, proxy, database := policyServiceFixture(t)
	agent.applyErr = errors.New("timeout")
	if _, err := service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "first"); err == nil {
		t.Fatal("expected pending activation")
	}
	if _, _, err := proxy.Update(context.Background(), "site_policy", "legacy", proxyPolicyUpdate(true, "/should-not-save"), "blocked"); err == nil {
		t.Fatal("pending first activation allowed proxy mutation")
	}
	if _, err := proxy.Sync(context.Background(), "site_policy", "blocked"); err == nil {
		t.Fatal("ordinary proxy sync allowed pending activation")
	}
	stored, err := repo.NewProxyRepo(database).GetByID("legacy")
	if err != nil || stored.LocationPath != "/" {
		t.Fatalf("rejected proxy edit changed desired state: %#v, %v", stored, err)
	}
}

func TestServiceRejectsEditedLinkedProxyPath(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	for _, change := range []func(*Rule){
		func(r *Rule) { r.Conditions[0].Values = []string{"/different"} },
		func(r *Rule) { r.Conditions[0].Negate = true },
		func(r *Rule) { r.Conditions[0].Operator = "exact" },
		func(r *Rule) { r.Match = "any" },
	} {
		p := linkedProxyPolicy()
		change(&p.Rules[0])
		if _, err := service.Save(context.Background(), "site_policy", p, "invalid-linked-path"); err == nil {
			t.Fatal("edited linked path was silently rewritten")
		}
		if _, err := service.Preview("site_policy", p, PreviewRequest{IP: "192.0.2.1", Path: "/"}); err == nil {
			t.Fatal("preview silently rewrote edited linked path")
		}
	}
	if len(agent.transactions) != 0 {
		t.Fatal("invalid linked path reached Agent")
	}
	if p, err := service.store.Get("site_policy"); err != nil || p != nil {
		t.Fatalf("invalid linked path persisted: %#v, %v", p, err)
	}
}
