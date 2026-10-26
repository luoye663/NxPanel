package maintenance

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

type fakeAgent struct {
	runtime         string
	runtimeWarnings string
	files           map[string]string
	calls           int
	err             error
	failAfter       bool
}

func (a *fakeAgent) NginxDump(context.Context, *agentclient.NginxDumpRequest) (*agentclient.NginxDumpResponse, error) {
	return &agentclient.NginxDumpResponse{Stdout: a.runtime, Stderr: a.runtimeWarnings}, nil
}

func (a *fakeAgent) ReadFile(_ context.Context, path string) ([]byte, string, error) {
	s, ok := a.files[path]
	if !ok {
		return nil, "", fmt.Errorf("missing file")
	}
	return []byte(s), nginxpatch.Hash(s), nil
}
func (a *fakeAgent) ApplyTransaction(_ context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	a.calls++
	if !req.TestNginx || !req.ReloadNginx {
		return nil, fmt.Errorf("missing required validation")
	}
	if a.err != nil && !a.failAfter {
		return nil, a.err
	}
	for _, change := range req.Changes {
		if change.ExpectedHash != "" && nginxpatch.Hash(a.files[change.Path]) != change.ExpectedHash {
			return nil, fmt.Errorf("hash conflict")
		}
	}
	for _, change := range req.Changes {
		if change.Type == "check" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(change.ContentBase64)
		if err != nil {
			return nil, err
		}
		a.files[change.Path] = string(b)
	}
	return &agentclient.TransactionResponse{}, a.err
}
func fixture(t *testing.T) (*Service, *fakeAgent, *sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	sites := repo.NewSiteRepo(database)
	a := &fakeAgent{files: map[string]string{}}
	for _, id := range []string{"one", "two"} {
		domain := id + ".example.com"
		conf := "/config/" + id + ".conf"
		if err := sites.Create(&repo.Site{ID: id, PrimaryDomain: domain, DomainsJSON: `["` + domain + `"]`, Status: "enabled", ConfigPath: conf, EnabledPath: conf, RewritePath: "/config/" + id + ".rewrite", RootPath: "/www", HTTPPort: 80, HTTPSPort: 443}); err != nil {
			t.Fatal(err)
		}
		a.files[conf] = `server {listen 80; listen 443 ssl; server_name ` + domain + `; ssl_certificate /external/cert; ssl_certificate_key /external/key; location / { proxy_pass http://backend; } }`
	}
	s := NewService(repo.NewMaintenanceRepo(database), sites, repo.NewOperationRepo(database), a, "/panel")
	return s, a, database, path
}
func set(t *testing.T, s *Service, id string, enabled bool, html *string) {
	t.Helper()
	state, err := s.Status(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(context.Background(), id, &UpdateRequest{Enabled: enabled, HTMLOverride: html, Version: state.Version, ExpectedHash: state.FileHash}, "test"); err != nil {
		t.Fatal(err)
	}
}

func TestMaintenanceLifecycleInheritanceAndCertificatePreservation(t *testing.T) {
	s, a, _, _ := fixture(t)
	ctx := context.Background()
	base := a.files["/config/one.conf"]
	set(t, s, "one", true, nil)
	state, err := s.Status(ctx, "one")
	if err != nil || !state.Enabled || state.ApplyStatus != "synced" || state.Drifted {
		t.Fatalf("status=%+v %v", state, err)
	}
	if a.files[s.pagePath("one")] != BuiltinHTML {
		t.Fatal("did not inherit builtin default")
	}
	custom := "<h1>custom</h1>"
	set(t, s, "two", true, &custom)
	defaults, _ := s.Defaults(ctx)
	global := "<h1>global</h1>"
	if _, err := s.SetDefaults(ctx, &DefaultsRequest{HTML: &global, Version: defaults.Version}, "test"); err != nil {
		t.Fatal(err)
	}
	if a.files[s.pagePath("one")] != global || a.files[s.pagePath("two")] != custom {
		t.Fatal("global update ignored inheritance or overwrote site override")
	}
	set(t, s, "two", true, nil)
	if a.files[s.pagePath("two")] != global {
		t.Fatal("reset to global default failed")
	}
	updated, err := nginxpatch.ReplaceCertificate(a.files["/config/one.conf"], "one.example.com", nginxpatch.Hash(a.files["/config/one.conf"]), "/panel/new/cert", "/panel/new/key")
	if err != nil {
		t.Fatal(err)
	}
	a.files["/config/one.conf"] = updated
	set(t, s, "one", false, nil)
	want, _ := nginxpatch.ReplaceCertificate(base, "one.example.com", nginxpatch.Hash(base), "/panel/new/cert", "/panel/new/key")
	if a.files["/config/one.conf"] != want {
		t.Fatal("restore replaced newer certificate with a stale snapshot")
	}
}

func TestMaintenanceUnknownOutcomeSurvivesRestartAndReconciles(t *testing.T) {
	s, a, database, path := fixture(t)
	ctx := context.Background()
	state, _ := s.Status(ctx, "one")
	a.err = fmt.Errorf("transport timeout")
	a.failAfter = true
	if _, err := s.Update(ctx, "one", &UpdateRequest{Enabled: true, Version: state.Version, ExpectedHash: state.FileHash}, "test"); err == nil {
		t.Fatal("ignored unknown result")
	}
	state, err := s.Status(ctx, "one")
	if err != nil || state.ApplyStatus != "pending" || state.Enabled || !state.DesiredEnabled {
		t.Fatalf("unknown outcome: %+v %v", state, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s = NewService(repo.NewMaintenanceRepo(reopened), repo.NewSiteRepo(reopened), repo.NewOperationRepo(reopened), a, "/panel")
	state, err = s.Status(ctx, "one")
	if err != nil || state.ApplyStatus != "pending" {
		t.Fatalf("lost durable state: %+v %v", state, err)
	}
	a.err = nil
	set(t, s, "one", true, nil)
	state, _ = s.Status(ctx, "one")
	if !state.Enabled || state.ApplyStatus != "synced" {
		t.Fatal("reconciliation failed")
	}
}

func TestMaintenanceDatabaseFailureBeforeAndAfterApply(t *testing.T) {
	s, a, database, _ := fixture(t)
	ctx := context.Background()
	state, _ := s.Status(ctx, "one")
	if _, err := database.Exec(`CREATE TRIGGER fail_op BEFORE INSERT ON operations BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	req := &UpdateRequest{Enabled: true, Version: state.Version, ExpectedHash: state.FileHash}
	if _, err := s.Update(ctx, "one", req, "test"); err == nil {
		t.Fatal("ignored database failure")
	}
	if a.calls != 0 {
		t.Fatal("applied without durable operation")
	}
	if _, err := database.Exec(`DROP TRIGGER fail_op; CREATE TRIGGER fail_confirm BEFORE UPDATE OF maintenance_enabled ON sites BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, "one", req, "test"); err == nil {
		t.Fatal("ignored confirmation failure")
	}
	state, _ = s.Status(ctx, "one")
	if state.ApplyStatus != "pending" || !nginxpatch.HasMaintenance(a.files["/config/one.conf"]) {
		t.Fatal("lost pending state after applied config")
	}
	if _, err := database.Exec(`DROP TRIGGER fail_confirm`); err != nil {
		t.Fatal(err)
	}
	set(t, s, "one", false, nil)
	if nginxpatch.HasMaintenance(a.files["/config/one.conf"]) {
		t.Fatal("failed to recover after DB confirmation failure")
	}
}

func TestMaintenanceConflictsDisabledAndHTMLBudget(t *testing.T) {
	s, a, database, _ := fixture(t)
	ctx := context.Background()
	state, _ := s.Status(ctx, "one")
	a.files["/config/one.conf"] += "\n# concurrent edit"
	if _, err := s.Update(ctx, "one", &UpdateRequest{Enabled: true, Version: state.Version, ExpectedHash: state.FileHash}, "test"); err == nil {
		t.Fatal("accepted stale hash")
	}
	if err := repo.NewSiteRepo(database).UpdateStatus("one", "disabled"); err != nil {
		t.Fatal(err)
	}
	state, _ = s.Status(ctx, "one")
	if _, err := s.Update(ctx, "one", &UpdateRequest{Enabled: true, Version: state.Version, ExpectedHash: state.FileHash}, "test"); err == nil {
		t.Fatal("enabled maintenance for disabled site")
	}
	html := strings.Repeat("x", MaxHTMLBytes+1)
	if _, err := s.Update(ctx, "two", &UpdateRequest{HTMLOverride: &html}, "test"); err == nil {
		t.Fatal("accepted oversized HTML")
	}
	max := strings.Repeat("x", MaxHTMLBytes)
	if err := validateHTML(&max); err != nil {
		t.Fatal(err)
	}
	set(t, s, "two", true, nil)
	blocked, err := s.store.Blocked(ctx, "two")
	if err != nil || !blocked {
		t.Fatal("active maintenance did not block conflicting writes")
	}
}

func TestMaintenanceRejectsInheritedRuntimeScriptHooks(t *testing.T) {
	s, a, _, _ := fixture(t)
	ctx := context.Background()
	state, err := s.Status(ctx, "one")
	if err != nil {
		t.Fatal(err)
	}
	a.runtime = `http { server_rewrite_by_lua_block { ngx.say("business bypass") } }`
	if _, err := s.Update(ctx, "one", &UpdateRequest{Enabled: true, Version: state.Version, ExpectedHash: state.FileHash}, "test"); err == nil {
		t.Fatal("accepted an inherited OpenResty hook before the maintenance guard")
	}
	if a.calls != 0 {
		t.Fatal("unsafe runtime was applied")
	}
	a.runtime = ""
	a.runtimeWarnings = `nginx: [warn] conflicting server name "one.example.com" on 0.0.0.0:80, ignored`
	if _, err := s.Update(ctx, "one", &UpdateRequest{Enabled: true, Version: state.Version, ExpectedHash: state.FileHash}, "test"); err == nil {
		t.Fatal("guessed a target despite a global server-name conflict")
	}
	if err := nginxpatch.ValidateMaintenanceRuntime("# server_rewrite_by_lua_block is only a comment\nhttp {}\n"); err != nil {
		t.Fatal(err)
	}
}
