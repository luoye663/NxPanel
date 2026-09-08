package accesspolicy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
)

func policyBackupArchive(t *testing.T, bundle *backupBundle) io.Reader {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	write := func(name string, raw []byte) {
		t.Helper()
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(raw); err != nil {
			t.Fatal(err)
		}
	}
	write("config/site.conf", []byte("server {}"))
	if bundle != nil {
		raw, err := json.Marshal(bundle)
		if err != nil {
			t.Fatal(err)
		}
		write("config/extra-4.conf", raw)
	}
	write("root/index.html", []byte("content"))
	write("metadata.json", []byte(`{"version":1,"site_id":"site_policy"}`))
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(out.Bytes())
}
func TestBackupRestoresPolicyAndAccountReferences(t *testing.T) {
	database := policyTestDB(t)
	svc := NewService(database, nil, nil, nil, "/panel")
	bundle := &backupBundle{Policy: Policy{Mode: "unified", DefaultAction: Action{Type: "auth", AccountIDs: []string{"historical"}}}, Accounts: []*repo.AuthAccount{{ID: "historical", Username: "backup-user", PasswordHash: "backup-user:{SHA}test", Enabled: true}}}
	called := false
	if err := svc.RestoreBackup(context.Background(), "site_policy", policyBackupArchive(t, bundle), func(paths []string) error {
		called = true
		if len(paths) != MaxRules+3 {
			t.Fatalf("fixed slot count %d", len(paths))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("agent restore not called")
	}
	got, err := svc.store.GetApplied("site_policy")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.DefaultAction.Type != "auth" || got.DefaultAction.AccountIDs[0] == "historical" {
		t.Fatalf("not remapped %#v", got)
	}
	account, err := svc.accounts.GetByID(got.DefaultAction.AccountIDs[0])
	if err != nil || account == nil || account.Scope != "site" || account.SiteID != "site_policy" || account.PasswordHash != "backup-user:{SHA}test" {
		t.Fatalf("restored account %#v %v", account, err)
	}
	if err := svc.WithBackup(context.Background(), "site_policy", func(paths []string) error {
		if paths[0] == "" {
			t.Fatal("unified backup omitted snapshot")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestBackupFailureAndAccountConflictPreserveState(t *testing.T) {
	database := policyTestDB(t)
	svc := NewService(database, nil, nil, nil, "/panel")
	p := Policy{DefaultAction: Action{Type: "allow"}}
	saved, err := svc.store.SaveDesired("site_policy", p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.MarkApplied("site_policy", saved.Version); err != nil {
		t.Fatal(err)
	}
	bundle := &backupBundle{Policy: Policy{DefaultAction: Action{Type: "deny", StatusCode: 451}}}
	marker := errors.New("nginx -t failed")
	if err := svc.RestoreBackup(context.Background(), "site_policy", policyBackupArchive(t, bundle), func([]string) error { return marker }); !errors.Is(err, marker) {
		t.Fatalf("failure %v", err)
	}
	got, _ := svc.store.GetApplied("site_policy")
	if got.DefaultAction.Type != "allow" {
		t.Fatal("failed restore changed live metadata")
	}
	account := &repo.AuthAccount{ID: "current", Scope: "global", Username: "same-user", PasswordHash: "same-user:{SHA}new", Enabled: true}
	if err := svc.accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	bundle = &backupBundle{Policy: Policy{DefaultAction: Action{Type: "auth", AccountIDs: []string{"old"}}}, Accounts: []*repo.AuthAccount{{ID: "old", Username: "same-user", PasswordHash: "same-user:{SHA}old", Enabled: true}}}
	called := false
	err = svc.RestoreBackup(context.Background(), "site_policy", policyBackupArchive(t, bundle), func([]string) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "冲突") || called {
		t.Fatalf("conflict must precede restore: %v called=%v", err, called)
	}
	current, _ := svc.accounts.GetByID("current")
	if current.PasswordHash != "same-user:{SHA}new" {
		t.Fatal("restore changed a global password")
	}
}
func TestLegacyBackupClearsUnifiedMetadataAndOmitsStaleFiles(t *testing.T) {
	svc := NewService(policyTestDB(t), nil, nil, nil, "/panel")
	saved, err := svc.store.SaveDesired("site_policy", Policy{DefaultAction: Action{Type: "deny"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.store.MarkApplied("site_policy", saved.Version); err != nil {
		t.Fatal(err)
	}
	if err := svc.RestoreBackup(context.Background(), "site_policy", policyBackupArchive(t, nil), func([]string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if svc.Managed("site_policy") {
		t.Fatal("old archive retained unified mode")
	}
	if err := svc.WithBackup(context.Background(), "site_policy", func(paths []string) error {
		for _, p := range paths {
			if p != "" {
				t.Fatalf("legacy backup included abandoned file %s", p)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestAuthSlotPathsStableAcrossReorder(t *testing.T) {
	p := samplePolicy()
	ctx := RenderContext{AuthFiles: map[string]string{"password": "user:{SHA}test\n"}}
	before, err := Compile("site", "/panel", p, ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.Rules[0], p.Rules[2] = p.Rules[2], p.Rules[0]
	after, err := Compile("site", "/panel", p, ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := AuthSlotPaths("site", "/panel")
	if before.Files[paths[2]] == "" || after.Files[paths[0]] == "" {
		t.Fatal("auth files do not use fixed ordinal slots")
	}
}
