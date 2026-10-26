package ssl

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

const discoveryConfig = `server { listen 443 ssl; server_name example.com; ssl_certificate /external/cert; ssl_certificate_key /external/key; }`

type discoveryAgent struct {
	runtime      string
	content      string
	reads        []string
	inspections  int
	writes       int
	inspectError error
	allowWrites  bool
}

func (a *discoveryAgent) NginxDump(context.Context) (string, error) { return a.runtime, nil }

func (a *discoveryAgent) SSLInspect(context.Context, string, string) (*SSLInspectResult, error) {
	panic("not used")
}
func (a *discoveryAgent) SSLInspectFiles(_ context.Context, cert, key string) (*SSLInspectResult, error) {
	a.inspections++
	if cert != "/external/cert" || key != "/external/key" {
		return nil, fmt.Errorf("unexpected file paths")
	}
	if a.inspectError != nil {
		return nil, a.inspectError
	}
	return &SSLInspectResult{Subject: "example.com", Issuer: "issuer", NotBefore: "2026-01-01", NotAfter: "2027-01-01", DNSNames: []string{"example.com"}, CertSHA256: "fingerprint", KeySHA256: "private-hash"}, nil
}
func (a *discoveryAgent) ApplyTransaction(context.Context, *TransactionRequest) error {
	a.writes++
	if a.allowWrites {
		return nil
	}
	return fmt.Errorf("read-only operation attempted write")
}

func TestManagedCertificateDeleteFailureRetainsMetadataForRetry(t *testing.T) {
	svc, a, database, _ := discoveryService(t)
	svc.cfg.Nginx.PanelDir = "/panel"
	c := &repo.Certificate{ID: "managed", Name: "managed", DomainsJSON: "[]", CertPath: "/panel/ssl-store/managed/cert", KeyPath: "/panel/ssl-store/managed/key"}
	store := repo.NewCertificateRepo(database)
	if err := store.Create(c); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteFromStore(context.Background(), c.ID, "request"); err == nil {
		t.Fatal("ignored Agent failure")
	}
	if got, err := store.GetByID(c.ID); err != nil || got == nil {
		t.Fatalf("lost retry metadata on Agent failure: %+v %v", got, err)
	}
	a.allowWrites = true
	if _, err := database.Exec(`CREATE TRIGGER fail_cert_delete BEFORE DELETE ON certificates BEGIN SELECT RAISE(ABORT, 'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteFromStore(context.Background(), c.ID, "request"); err == nil {
		t.Fatal("ignored DB failure after file removal")
	}
	if got, err := store.GetByID(c.ID); err != nil || got == nil {
		t.Fatalf("lost retry metadata on DB failure: %+v %v", got, err)
	}
	if _, err := database.Exec(`DROP TRIGGER fail_cert_delete`); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteFromStore(context.Background(), c.ID, "request"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetByID(c.ID); err != nil || got != nil {
		t.Fatalf("retry failed: %+v %v", got, err)
	}
}

func TestLegacyExternalCertificateForgetNeverDeletesFiles(t *testing.T) {
	svc, a, database, _ := discoveryService(t)
	svc.cfg.Nginx.PanelDir = "/panel"
	store := repo.NewCertificateRepo(database)
	// Before discovery, existing_files records had no explicit source column.
	if err := store.Create(&repo.Certificate{ID: "legacy", Name: "legacy", DomainsJSON: "[]", CertPath: "/external/cert", KeyPath: "/external/key"}); err != nil {
		t.Fatal(err)
	}
	listed, err := svc.ListStoreCertificates()
	if err != nil || len(listed) != 1 || listed[0].Source != "external" {
		t.Fatalf("legacy ownership: %+v %v", listed, err)
	}
	if _, err := svc.DeleteFromStore(context.Background(), "legacy", "request"); err != nil {
		t.Fatal(err)
	}
	if a.writes != 0 {
		t.Fatal("legacy external key/certificate deleted")
	}
}

func TestExternalForgetUnlinksMetadataWithoutDisablingExistingSSL(t *testing.T) {
	svc, a, _, _ := discoveryService(t)
	ctx := context.Background()
	if err := svc.sslRepo.Upsert(&repo.SiteSSL{SiteID: "site", Enabled: true, Mode: "existing_files", CertPath: "/external/cert", KeyPath: "/external/key", ForceHTTPS: true, HSTSEnabled: true}); err != nil {
		t.Fatal(err)
	}
	scan, err := svc.ScanCertificate(ctx, "site")
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := svc.AdoptCertificate(ctx, &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteFromStore(ctx, id, "test"); err != nil {
		t.Fatal(err)
	}
	row, err := svc.sslRepo.GetBySiteID("site")
	if err != nil || row.CertStoreID != nil || !row.Enabled || !row.ForceHTTPS || !row.HSTSEnabled || row.CertPath != "/external/cert" {
		t.Fatalf("forget changed site SSL configuration: %+v %v", row, err)
	}
	if a.writes != 0 {
		t.Fatal("external cancellation changed files")
	}
}

func TestDiscoveryRejectsInheritedScriptedTLS(t *testing.T) {
	svc, a, _, _ := discoveryService(t)
	a.runtime = `http {ssl_certificate_by_lua_file /global/select.lua;}`
	scan, err := svc.ScanCertificate(context.Background(), "site")
	if err != nil || scan.ReadOnlyReason == "" || scan.Certificate != nil {
		t.Fatalf("dynamic TLS must remain read-only: %+v %v", scan, err)
	}
	if a.inspections != 0 || a.writes != 0 {
		t.Fatal("dynamic TLS was inspected as a static certificate or changed")
	}
}
func (a *discoveryAgent) ReadFile(_ context.Context, path string) ([]byte, error) {
	a.reads = append(a.reads, path)
	return []byte(a.content), nil
}

func discoveryService(t *testing.T) (*Service, *discoveryAgent, *sql.DB, string) {
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
	if err := sites.Create(&repo.Site{ID: "site", PrimaryDomain: "example.com", DomainsJSON: `["example.com"]`, BindingsJSON: `["example.com"]`, Status: "enabled", HTTPPort: 80, HTTPSPort: 443, RootPath: "/www", ConfigPath: "/config/site.conf", EnabledPath: "/config/site.conf"}); err != nil {
		t.Fatal(err)
	}
	a := &discoveryAgent{content: discoveryConfig}
	svc := NewService(sites, repo.NewSSLRepo(database), repo.NewCertificateRepo(database), repo.NewOperationRepo(database), a, &app.Config{})
	return svc, a, database, path
}

func TestDiscoveryAdoptionReadOnlyAndRestart(t *testing.T) {
	svc, a, database, path := discoveryService(t)
	scan, err := svc.ScanCertificate(context.Background(), "site")
	if err != nil || scan.Certificate == nil || scan.ReadOnlyReason != "" {
		t.Fatalf("scan=%+v %v", scan, err)
	}
	encoded, _ := json.Marshal(scan)
	if strings.Contains(string(encoded), "private-hash") || strings.Contains(string(encoded), "PRIVATE KEY") {
		t.Fatal("private data in scan")
	}
	id, _, err := svc.AdoptCertificate(context.Background(), &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "request")
	if err != nil || id == "" {
		t.Fatalf("adopt: %s %v", id, err)
	}
	if a.writes != 0 {
		t.Fatal("adoption wrote files")
	}
	for _, p := range a.reads {
		if p != "/config/site.conf" {
			t.Fatalf("API read certificate or key: %s", p)
		}
	}
	if ssl, err := repo.NewSSLRepo(database).GetBySiteID("site"); err != nil || ssl != nil {
		t.Fatal("metadata adoption changed site_ssl")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stored, err := repo.NewCertificateRepo(reopened).GetByID(id)
	if err != nil || stored == nil || stored.Source != "external" {
		t.Fatalf("restart: %+v %v", stored, err)
	}
	sites, err := repo.NewCertificateRepo(reopened).AssociatedSites(id)
	if err != nil || len(sites) != 1 {
		t.Fatalf("lost association: %+v %v", sites, err)
	}
}

func TestDiscoveryRejectsComplexAndConflictingConfirmation(t *testing.T) {
	svc, a, _, _ := discoveryService(t)
	scan, err := svc.ScanCertificate(context.Background(), "site")
	if err != nil {
		t.Fatal(err)
	}
	a.content += "\n# changed"
	if _, _, err := svc.AdoptCertificate(context.Background(), &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "request"); err == nil {
		t.Fatal("accepted stale scan")
	}
	a.content = strings.Replace(discoveryConfig, "listen 443 ssl;", "listen 443 ssl; include /external/ssl.conf;", 1)
	a.inspections = 0
	scan, err = svc.ScanCertificate(context.Background(), "site")
	if err != nil || scan.ReadOnlyReason == "" || a.inspections != 0 {
		t.Fatalf("unsafe scan=%+v err=%v inspections=%d", scan, err, a.inspections)
	}
	if _, _, err := svc.AdoptCertificate(context.Background(), &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "request"); err == nil {
		t.Fatal("adopted complex configuration")
	}
	if a.writes != 0 {
		t.Fatal("read-only scan wrote files")
	}
}

func TestDiscoverySanitizesInspectorErrorAndForgetPreservesFiles(t *testing.T) {
	svc, a, _, _ := discoveryService(t)
	a.inspectError = fmt.Errorf("-----BEGIN PRIVATE KEY----- sensitive agent diagnostic")
	scan, err := svc.ScanCertificate(context.Background(), "site")
	if err != nil || scan.ReadOnlyReason == "" || strings.Contains(scan.ReadOnlyReason, "PRIVATE KEY") {
		t.Fatalf("error leak: %+v %v", scan, err)
	}
	a.inspectError = nil
	scan, _ = svc.ScanCertificate(context.Background(), "site")
	id, _, err := svc.AdoptCertificate(context.Background(), &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "request")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.DeleteFromStore(context.Background(), id, "request"); err != nil {
		t.Fatal(err)
	}
	if a.writes != 0 {
		t.Fatal("forget attempted to remove external files")
	}
	certs, err := svc.ListStoreCertificates()
	if err != nil || len(certs) != 0 {
		t.Fatalf("forget metadata: %+v %v", certs, err)
	}
}

func TestCertificateAdoptionStatusLifecycle(t *testing.T) {
	svc, a, database, _ := discoveryService(t)
	ctx := context.Background()
	assertStatus := func(want string) *CertificateScan {
		t.Helper()
		scan, err := svc.ScanCertificate(ctx, "site")
		if err != nil || scan.AdoptionStatus != want {
			t.Fatalf("want %s: %+v %v", want, scan, err)
		}
		return scan
	}
	scan := assertStatus("unadopted")
	id, _, err := svc.AdoptCertificate(ctx, &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if scan = assertStatus("adopted"); scan.CertificateID != id {
		t.Fatal("missing certificate ID")
	}
	// Reconstructing the service must recover state from the persisted association.
	svc = NewService(repo.NewSiteRepo(database), repo.NewSSLRepo(database), repo.NewCertificateRepo(database), repo.NewOperationRepo(database), a, &app.Config{})
	assertStatus("adopted")
	a.content += "\n# external edit"
	scan = assertStatus("changed")
	id2, _, err := svc.AdoptCertificate(ctx, &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "test")
	if err != nil || id != id2 {
		t.Fatalf("repeat adoption: %s %v", id2, err)
	}
	assertStatus("adopted")
	if _, err := database.Exec(`UPDATE certificates SET cert_sha256='old' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	assertStatus("changed")
	a.inspectError = fmt.Errorf("unreadable")
	assertStatus("unknown")
	a.inspectError = nil
	if _, err := svc.DeleteFromStore(ctx, id, "test"); err != nil {
		t.Fatal(err)
	}
	assertStatus("unadopted")
	if a.writes != 0 {
		t.Fatal("scan/adoption changed nginx")
	}
}
