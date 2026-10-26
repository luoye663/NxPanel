package accesspolicy

import (
	"context"
	"errors"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

func TestSaveFailureReportsWhetherEditorDraftWasPersisted(t *testing.T) {
	for _, beforeSave := range []bool{true, false} {
		t.Run(map[bool]string{true: "read failure", false: "transaction failure"}[beforeSave], func(t *testing.T) {
			svc, agent, _, _ := policyServiceFixture(t)
			if beforeSave {
				delete(agent.files, "/tmp/site.conf")
			} else {
				agent.applyErr = errors.New("transaction connection lost")
			}
			_, err := svc.Save(context.Background(), "site_policy", serviceAllowPolicy(), "save")
			var ae *app.AppError
			if !errors.As(err, &ae) {
				t.Fatalf("expected app error: %v", err)
			}
			if ae.Details["desired_saved"] != !beforeSave || ae.Details["sync_required"] != !beforeSave {
				t.Fatalf("wrong recovery details: %#v", ae.Details)
			}
			p, readErr := svc.store.Get("site_policy")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if beforeSave && p != nil {
				t.Fatal("read failure stored a draft")
			}
			if !beforeSave && (p == nil || ae.Details["saved_version"] != p.Version) {
				t.Fatalf("missing persisted version: %#v %#v", p, ae.Details)
			}
		})
	}
}

func TestInactiveRuleCannotCaptureAnotherSitesAccount(t *testing.T) {
	svc, agent, _, database := policyServiceFixture(t)
	if _, err := database.Exec(`INSERT INTO sites(id,primary_domain,domains_json,status,root_path,access_log_path,error_log_path,config_path,enabled_path,rewrite_path) VALUES('other','other.test','[]','enabled','/tmp','/tmp/a','/tmp/e','/tmp/other','/tmp/n','/tmp/r')`); err != nil {
		t.Fatal(err)
	}
	if err := repo.NewAuthAccountRepo(database).Create(&repo.AuthAccount{ID: "foreign", Scope: "site", SiteID: "other", Username: "foreign", PasswordHash: "foreign:{SHA}hash", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	p := serviceAllowPolicy()
	p.Rules = []Rule{{ID: "inactive", Name: "disabled rule", Enabled: false, Match: "all", Action: Action{Type: "auth", AccountIDs: []string{"foreign"}}}}
	if _, err := svc.Save(context.Background(), "site_policy", p, "save"); err == nil {
		t.Fatal("captured foreign account in disabled rule snapshot")
	}
	if len(agent.transactions) != 0 {
		t.Fatal("invalid policy reached agent")
	}
	if stored, err := svc.store.Get("site_policy"); err != nil || stored != nil {
		t.Fatalf("invalid policy persisted: %#v %v", stored, err)
	}
}
