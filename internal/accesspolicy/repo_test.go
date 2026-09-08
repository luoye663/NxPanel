package accesspolicy

import (
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/luoye663/nxpanel/internal/db"
)

func policyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = database.Close() })
	if err := db.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sites
		(id, primary_domain, domains_json, status, root_path, access_log_path, error_log_path,
		 config_path, enabled_path, rewrite_path)
		VALUES ('site_policy', 'policy.example.com', '["policy.example.com"]', 'enabled', '/tmp',
		'/tmp/access.log', '/tmp/error.log', '/tmp/site.conf', '/tmp/site.enabled', '/tmp/site.rewrite')`); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestRepoDesiredAndAppliedRemainSeparate(t *testing.T) {
	r := NewRepo(policyTestDB(t))
	const siteID = "site_policy"
	if policy, err := r.Get(siteID); err != nil || policy != nil {
		t.Fatalf("absent policy: %#v, %v", policy, err)
	}
	if mode, err := r.Mode(siteID); err != nil || mode != "legacy" {
		t.Fatalf("absent mode: %q, %v", mode, err)
	}
	desired := Policy{Mode: "unified", DefaultAction: Action{Type: "deny", StatusCode: 451, ResponseBody: "first"}}
	saved, err := r.SaveDesired(siteID, desired, 0)
	if err != nil || saved.Version != 1 || saved.Mode != "legacy" || saved.ApplyStatus != "pending" {
		t.Fatalf("save must keep live legacy mode: %#v, %v", saved, err)
	}
	if _, err := r.SaveDesired(siteID, desired, 0); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("duplicate first save: %v", err)
	}
	if err := r.MarkError(siteID, 1, "transport timeout"); err != nil {
		t.Fatal(err)
	}
	saved, err = r.Get(siteID)
	if err != nil || saved.DefaultAction.ResponseBody != "first" || saved.Mode != "legacy" || saved.LastError != "transport timeout" {
		t.Fatalf("uncertain failure must preserve desired and mode: %#v, %v", saved, err)
	}
	if applied, err := r.GetApplied(siteID); err != nil || applied != nil {
		t.Fatalf("unconfirmed activation has no applied snapshot: %#v, %v", applied, err)
	}
	if err := r.MarkApplied(siteID, 1); err != nil {
		t.Fatal(err)
	}
	if mode, err := r.Mode(siteID); err != nil || mode != "unified" {
		t.Fatalf("activation: %q, %v", mode, err)
	}
	desired.DefaultAction.ResponseBody = "second"
	saved, err = r.SaveDesired(siteID, desired, 1)
	if err != nil || saved.Version != 2 || saved.Mode != "unified" {
		t.Fatalf("update active policy: %#v, %v", saved, err)
	}
	if err := r.MarkApplied(siteID, 1); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale completion: %v", err)
	}
	if err := r.MarkError(siteID, 1, "stale failure"); !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale error: %v", err)
	}
	if err := r.MarkError(siteID, 2, "nginx -t failed"); err != nil {
		t.Fatal(err)
	}
	applied, err := r.GetApplied(siteID)
	if err != nil || applied.Version != 1 || applied.DefaultAction.ResponseBody != "first" || applied.ApplyStatus != "applied" {
		t.Fatalf("previous applied snapshot lost: %#v, %v", applied, err)
	}
	items, err := r.List()
	if err != nil || len(items) != 1 || items[0].Policy.Version != 2 || items[0].Policy.LastError != "nginx -t failed" {
		t.Fatalf("list pending desired: %#v, %v", items, err)
	}
}

func TestRepoConcurrentCAS(t *testing.T) {
	r := NewRepo(policyTestDB(t))
	p := Policy{DefaultAction: Action{Type: "allow"}}
	if _, err := r.SaveDesired("site_policy", p, 0); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < cap(results); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := r.SaveDesired("site_policy", p, 1)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		} else if !errors.Is(err, ErrVersionConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("expected one successful editor, got %d", winners)
	}
}

func TestRepoSiteForeignKeyAndCascade(t *testing.T) {
	database := policyTestDB(t)
	r := NewRepo(database)
	p := Policy{DefaultAction: Action{Type: "allow"}}
	if _, err := r.SaveDesired("missing", p, 0); err == nil {
		t.Fatal("policy without site must fail")
	}
	if _, err := r.SaveDesired("site_policy", p, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM sites WHERE id='site_policy'`); err != nil {
		t.Fatal(err)
	}
	if policy, err := r.Get("site_policy"); err != nil || policy != nil {
		t.Fatalf("policy not cascaded: %#v, %v", policy, err)
	}
}
