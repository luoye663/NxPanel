package proxy

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestStripGeneratedProxyAuthPreservesCustomContent(t *testing.T) {
	config := []byte(`server {
    auth_basic "custom server auth";
    #NXPANEL-MAIN-LOCATION-START
    location / {
        auth_basic "Restricted";
        auth_basic_user_file /panel/proxy-auth/p1.htpasswd;
        proxy_pass http://backend;
        add_header X-Custom "keep me";
    }
    #NXPANEL-MAIN-LOCATION-END
    #NXPANEL-EXTRA-LOCATIONS-START
    location /custom {
        auth_basic "Restricted";
        auth_basic_user_file /custom/not-managed.htpasswd;
        proxy_pass http://other;
    }
    #NXPANEL-EXTRA-LOCATIONS-END
    location /outside {
        auth_basic "Restricted";
        auth_basic_user_file /panel/proxy-auth/p1.htpasswd;
    }
}
`)
	proxies := []*repo.SiteProxy{{ID: "p1", AuthEnabled: true, AuthHtpasswdPath: "/panel/proxy-auth/p1.htpasswd"}}
	got, err := stripGeneratedProxyAuth(config, proxies, "/panel")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(string(config), "        auth_basic \"Restricted\";\n        auth_basic_user_file /panel/proxy-auth/p1.htpasswd;\n", "", 1)
	if string(got) != want {
		t.Fatalf("must only strip known pair inside proxy marker:\n%s", got)
	}
	if again, err := stripGeneratedProxyAuth(got, proxies, "/panel"); err != nil || string(again) != string(got) {
		t.Fatalf("strip not idempotent: %v", err)
	}
}

func TestWithAccessPolicyConfigDoesNotWriteBeforeCallback(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n}\n")}
	service, _, _, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site")
	callbackErr := errors.New("policy preflight failed")
	err := service.WithAccessPolicyConfig(context.Background(), "site", func(content []byte) error {
		if string(content) != string(agent.config) {
			t.Fatal("unexpected config")
		}
		return callbackErr
	})
	if !errors.Is(err, callbackErr) || len(agent.transactions) != 0 {
		t.Fatalf("callback failure wrote config: %v", err)
	}
}

func TestManagedProxyRejectsIndependentAuth(t *testing.T) {
	service, _, _, _, _ := setupProxyService(t, &proxyTestAgent{})
	service.SetAccessPolicyHooks(func(string) bool { return true }, nil)
	for _, withIDs := range []bool{false, true} {
		request := directCreateRequest("api", "/api")
		request.AuthEnabled = !withIDs
		if withIDs {
			request.AuthAccountIDs = []string{"account"}
		}
		if _, _, err := service.Create(context.Background(), "site", request, "request"); err == nil || !strings.Contains(err.Error(), "统一访问策略") {
			t.Fatalf("independent auth create accepted: %v", err)
		}
	}
}

func TestManagedProxyUpdatePreservesReferencesAndSharesTransaction(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n#NXPANEL-MAIN-LOCATION-START\nlocation / { return 200; }\n#NXPANEL-MAIN-LOCATION-END\n#NXPANEL-EXTRA-LOCATIONS-START\n#NXPANEL-EXTRA-LOCATIONS-END\n}\n")}
	service, _, proxies, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site")
	if err := service.accountRepo.Create(&repo.AuthAccount{ID: "account", Scope: "global", Username: "user", PasswordHash: "{SHA}value", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := proxies.Create(&repo.SiteProxy{ID: "proxy", SiteID: "site", Name: "api", Enabled: true,
		LocationPath: "/api", UpstreamURL: "http://backend", AuthEnabled: true, AuthHtpasswdPath: "/panel/auth/proxy.htpasswd"}); err != nil {
		t.Fatal(err)
	}
	if err := proxies.SetAccountIDs("proxy", []string{"account"}); err != nil {
		t.Fatal(err)
	}
	prepared, finished := false, false
	service.SetAccessPolicyHooks(func(string) bool { return true }, func(_ context.Context, site *repo.Site, desired []*repo.SiteProxy, changes []agentclient.FileChangeRequest) ([]agentclient.FileChangeRequest, func(error) error, error) {
		prepared = true
		if site.ID != "site" || len(desired) != 1 || desired[0].LocationPath != "/new-api" || desired[0].Enabled {
			t.Fatalf("hook missing desired change: %#v", desired)
		}
		changes = append(changes, agentclient.FileChangeRequest{Type: "write", Path: "/policy.conf", ContentBase64: base64.StdEncoding.EncodeToString([]byte("policy"))})
		return changes, func(err error) error {
			finished = true
			if err != nil {
				t.Fatalf("unexpected transaction error: %v", err)
			}
			return nil
		}, nil
	})
	_, _, err := service.Update(context.Background(), "site", "proxy", &UpdateProxyRequest{Name: "api", LocationPath: "/new-api", UpstreamURL: "http://backend:8080"}, "request")
	if err != nil {
		t.Fatal(err)
	}
	if !prepared || !finished || len(agent.transactions) != 1 {
		t.Fatal("policy and proxy must share one completed transaction")
	}
	persisted, err := proxies.GetByID("proxy")
	if err != nil || !persisted.AuthEnabled {
		t.Fatalf("legacy account metadata lost: %#v, %v", persisted, err)
	}
	ids, err := proxies.GetAccountIDs("proxy")
	if err != nil || !reflect.DeepEqual(ids, []string{"account"}) {
		t.Fatalf("account references lost: %#v, %v", ids, err)
	}
	for _, change := range agent.transactions[0].Changes {
		if change.Path == "/panel/auth/proxy.htpasswd" {
			t.Fatal("independent old htpasswd unexpectedly rewritten")
		}
	}
}

func TestPolicyHookReceivesAgentFailure(t *testing.T) {
	failure := errors.New("socket timeout")
	agent := &proxyTestAgent{applyErr: failure, config: []byte("server {\n#NXPANEL-MAIN-LOCATION-START\n#NXPANEL-MAIN-LOCATION-END\n#NXPANEL-EXTRA-LOCATIONS-START\n#NXPANEL-EXTRA-LOCATIONS-END\n}\n")}
	service, _, _, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site")
	finished := false
	service.SetAccessPolicyHooks(func(string) bool { return true }, func(_ context.Context, _ *repo.Site, _ []*repo.SiteProxy, changes []agentclient.FileChangeRequest) ([]agentclient.FileChangeRequest, func(error) error, error) {
		return changes, func(err error) error {
			finished = true
			if !errors.Is(err, failure) {
				t.Fatalf("wrong hook outcome: %v", err)
			}
			return nil
		}, nil
	})
	if _, _, err := service.Create(context.Background(), "site", directCreateRequest("api", "/api"), "request"); err == nil || !finished {
		t.Fatalf("failed apply must finish policy callback: %v", err)
	}
}

func TestManagedProxySyncInheritsUnifiedAuthentication(t *testing.T) {
	agent := &proxyTestAgent{config: []byte("server {\n#NXPANEL-MAIN-LOCATION-START\n#NXPANEL-MAIN-LOCATION-END\n#NXPANEL-EXTRA-LOCATIONS-START\n#NXPANEL-EXTRA-LOCATIONS-END\n}\n")}
	service, _, proxies, _, _ := setupProxyService(t, agent)
	createProxyServiceSite(t, service, "site")
	if err := proxies.Create(&repo.SiteProxy{ID: "proxy", SiteID: "site", Name: "api", Enabled: true,
		LocationPath: "/api", UpstreamURL: "http://backend", AuthEnabled: true, AuthHtpasswdPath: "/panel/auth/proxy.htpasswd"}); err != nil {
		t.Fatal(err)
	}
	service.SetAccessPolicyHooks(func(string) bool { return true }, nil)
	if _, err := service.Sync(context.Background(), "site", "request"); err != nil {
		t.Fatal(err)
	}
	if len(agent.transactions) != 1 {
		t.Fatal("missing transaction")
	}
	content, err := base64.StdEncoding.DecodeString(agent.transactions[0].Changes[0].ContentBase64)
	if err != nil || strings.Contains(string(content), "auth_basic") || !strings.Contains(string(content), "proxy_pass http://backend;") {
		t.Fatalf("proxy must keep handler and inherit server policy: %s, %v", content, err)
	}
	for _, change := range agent.transactions[0].Changes {
		if change.Path == "/panel/auth/proxy.htpasswd" {
			t.Fatal("managed sync must not rewrite legacy password file")
		}
	}
}
