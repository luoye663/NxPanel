package accesspolicy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/configpreflight"
	"github.com/luoye663/nxpanel/internal/nginx"
)

func TestSaveAndSyncPreserveCustomCORSInclude(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	body, err := os.ReadFile("../configpreflight/testdata/cors.conf")
	if err != nil {
		t.Fatal(err)
	}
	main, err := nginx.ReplaceMarkerBlock([]byte(servicePolicyConfig), nginx.MarkerNameMainLocation, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := "/tmp/rewrite.conf"
	agent.files[path] = body
	agent.files["/tmp/site.conf"] = []byte(strings.Replace(string(main), `    add_header X-Server`, "    include "+path+";\n    add_header X-Server", 1))
	for _, run := range []func() error{
		func() error {
			_, err := service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "save")
			return err
		},
		func() error { _, err := service.Sync(context.Background(), "site_policy", "sync"); return err },
	} {
		if err := run(); err != nil {
			t.Fatal(err)
		}
		report, err := service.DiagnoseSite(context.Background(), "site_policy")
		if err != nil || !report.Complete || len(report.Warnings) != 1 || report.Warnings[0].Path != path || report.Warnings[0].Line != 32 {
			t.Fatalf("CORS diagnostic: %+v %v", report, err)
		}
		if !bytes.Equal(agent.files[path], body) || !strings.Contains(string(agent.files["/tmp/site.conf"]), "include "+path+";") {
			t.Fatal("custom CORS configuration changed")
		}
	}
}

func TestDiagnosticsPreservesSourceLines(t *testing.T) {
	service, agent, site := configGuardFixture()
	agent.files["/etc/nginx/nginx.conf"] = []byte("http {}")
	main := []byte("server {\n#NXPANEL-ACCESS-LIMIT-START\ndeny all;\n\n#NXPANEL-ACCESS-LIMIT-END\n#NXPANEL-FORCE-HTTPS-START\nreturn 301 https://example.test;\n#NXPANEL-FORCE-HTTPS-END\nreturn 204;\n}\n")
	report := service.DiagnoseConfig(context.Background(), site, main)
	if !report.Complete || len(report.Warnings) != 1 || report.Warnings[0].Line != 9 {
		t.Fatalf("shifted source: %+v", report)
	}
	if bytes.Count(main, []byte("\n")) != bytes.Count(stripOwnedAccessBlocks(main), []byte("\n")) {
		t.Fatal("source lines removed")
	}
}

type unavailableDiagnosticAgent struct {
	*policyServiceAgent
	calls int
}

func (a *unavailableDiagnosticAgent) DiagnoseAccessConfig(context.Context, *agentclient.ConfigPreflightRequest) (configpreflight.Report, error) {
	a.calls++
	return configpreflight.Report{}, fmt.Errorf("diagnostic timeout")
}

func TestDiagnosticFailureDoesNotGateSaveSyncOrRefresh(t *testing.T) {
	service, agent, proxy, _ := policyServiceFixture(t)
	unavailable := &unavailableDiagnosticAgent{policyServiceAgent: agent}
	service.agent = unavailable
	report, err := service.DiagnoseSite(context.Background(), "site_policy")
	if err != nil || report.Complete || len(report.Warnings) != 1 || unavailable.calls != 1 {
		t.Fatalf("failure not reported: %+v %v", report, err)
	}
	if len(agent.transactions) != 0 {
		t.Fatal("diagnostic wrote files")
	}
	if p, _ := service.store.Get("site_policy"); p != nil {
		t.Fatal("diagnostic persisted draft")
	}
	if _, err := service.Save(context.Background(), "site_policy", serviceAllowPolicy(), "save"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(context.Background(), "site_policy", "sync"); err != nil {
		t.Fatal(err)
	}
	if err := service.RefreshActive(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := proxy.Update(context.Background(), "site_policy", "legacy", proxyPolicyUpdate(true, "/"), "proxy"); err != nil {
		t.Fatal(err)
	}
	if unavailable.calls != 1 {
		t.Fatal("write path called diagnostic")
	}
	for _, tx := range agent.transactions {
		if !tx.TestNginx {
			t.Fatal("write skipped nginx -t")
		}
	}
}

func TestDiagnosticProxyPreparationRetainsOriginalLines(t *testing.T) {
	service, agent, _, _ := policyServiceFixture(t)
	main := strings.Replace(servicePolicyConfig, `    add_header X-Server`, "    return 204;\n    add_header X-Server", 1)
	agent.files["/tmp/site.conf"] = []byte(main)
	report, err := service.DiagnoseSite(context.Background(), "site_policy")
	wantLine := strings.Count(main[:strings.Index(main, "    return 204;")], "\n") + 1
	if err != nil || !report.Complete || len(report.Warnings) != 1 || report.Warnings[0].Line != wantLine {
		t.Fatalf("proxy preparation shifted lines: %+v %v", report, err)
	}
	if string(agent.files["/tmp/site.conf"]) != main {
		t.Fatal("read-only diagnostic changed source")
	}
}
