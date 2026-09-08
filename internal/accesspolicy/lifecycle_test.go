package accesspolicy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/sites"
)

func TestSiteLifecyclePolicyUsesOriginalTransaction(t *testing.T) {
	database := policyTestDB(t)
	if err := nginx.InitTemplates("../../configs/templates"); err != nil {
		t.Fatal(err)
	}
	socketDir, err := os.MkdirTemp("/tmp", "nxpolicy-lifecycle-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	socket := filepath.Join(socketDir, "agent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	requests := make(chan agentclient.TransactionRequest, 4)
	var fail atomic.Bool
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/transactions/apply" {
			http.NotFound(w, r)
			return
		}
		var req agentclient.TransactionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		requests <- req
		w.Header().Set("Content-Type", "application/json")
		if fail.Load() {
			w.Write([]byte(`{"ok":false,"error":"nginx -t failed"}`))
			return
		}
		w.Write([]byte(`{"ok":true,"data":{}}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	agent := agentclient.NewWithDefaults(socket, "")
	defer agent.Close()
	cfg := &app.Config{Nginx: app.NginxConfig{PanelDir: "/panel", LogDir: "/logs"}}
	siteRepo := repo.NewSiteRepo(database)
	siteSvc := sites.NewService(database, siteRepo, repo.NewProxyRepo(database), repo.NewSSLRepo(database), repo.NewRewriteRepo(database), repo.NewOperationRepo(database), agent, cfg)
	policySvc := NewService(database, agent, nil, nil, "/panel")
	siteSvc.SetAccessPolicyHooks(policySvc.PrepareSiteCreate, policySvc.FinishSiteCreate, policySvc.SiteDeleteFiles)
	created, opID, err := siteSvc.Create(context.Background(), &sites.CreateSiteRequest{Bindings: []sites.Binding{{Domain: "fresh.example.com", Port: 80}}, RootPath: "/var/www/fresh", EnableAfterCreate: true}, "create-test")
	if err != nil {
		t.Fatal(err)
	}
	if opID == "" {
		t.Fatal("missing operation")
	}
	request := <-requests
	if !request.TestNginx || !request.ReloadNginx {
		t.Fatal("policy creation escaped site transaction checks")
	}
	writes := map[string]string{}
	for _, change := range request.Changes {
		if change.Type == "write" {
			if _, duplicate := writes[change.Path]; duplicate {
				t.Fatalf("duplicate write %s", change.Path)
			}
			raw, err := base64.StdEncoding.DecodeString(change.ContentBase64)
			if err != nil {
				t.Fatal(err)
			}
			writes[change.Path] = string(raw)
		}
	}
	if !strings.Contains(writes[created.AccessLimitPath], "_decision") || writes[policySvc.globalPath(created.ID)] == "" || writes[policySvc.bundlePath(created.ID)] == "" {
		t.Fatal("original transaction omitted policy dependencies")
	}
	if strings.Contains(writes[created.AccessLimitPath], "auth_basic off;") {
		t.Fatal("empty initial policy cancelled inherited authentication")
	}
	p, err := policySvc.store.GetApplied(created.ID)
	if err != nil || p == nil || p.Mode != "unified" || p.Version != 1 || p.DefaultAction.Type != "allow" || len(p.Rules) != 0 {
		t.Fatalf("new policy %#v %v", p, err)
	}
	if policySvc.Managed("site_policy") {
		t.Fatal("existing site implicitly migrated")
	}
	deleted, _, err := siteSvc.Delete(context.Background(), created.ID, &sites.DeleteSiteRequest{}, "delete-test")
	if err != nil || !deleted {
		t.Fatalf("delete %v", err)
	}
	deletion := <-requests
	removed := map[string]bool{}
	for _, change := range deletion.Changes {
		if change.Type == "remove" {
			removed[change.Path] = true
			if change.Path == "/panel/access-policy" {
				t.Fatal("recursive/shared policy directory removal")
			}
		}
	}
	for _, path := range policySvc.backupPaths(created.ID) {
		if !removed[path] {
			t.Fatalf("delete omitted policy file %s", path)
		}
	}
	if p, err := policySvc.store.Get(created.ID); err != nil || p != nil {
		t.Fatalf("delete retained metadata %#v %v", p, err)
	}
	fail.Store(true)
	if _, _, err := siteSvc.Create(context.Background(), &sites.CreateSiteRequest{Bindings: []sites.Binding{{Domain: "failed.example.com", Port: 80}}, RootPath: "/var/www/failed"}, "failed-create"); err == nil {
		t.Fatal("agent failure accepted")
	}
	<-requests
	if site, err := siteRepo.GetByPrimaryDomain("failed.example.com"); err != nil || site != nil {
		t.Fatalf("failed transaction published site %#v %v", site, err)
	}
}
