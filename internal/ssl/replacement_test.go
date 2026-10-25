package ssl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

type replacementAgent struct {
	files map[string]string
	last  *TransactionRequest
	fail  bool
}

func (a *replacementAgent) NginxDump(context.Context) (string, error) { return "", nil }

func (a *replacementAgent) ReadFile(_ context.Context, path string) ([]byte, error) {
	v, ok := a.files[path]
	if !ok {
		return nil, fmt.Errorf("missing file")
	}
	return []byte(v), nil
}
func (a *replacementAgent) SSLInspect(context.Context, string, string) (*SSLInspectResult, error) {
	panic("unused")
}
func (a *replacementAgent) SSLInspectFiles(_ context.Context, cert, key string) (*SSLInspectResult, error) {
	c, cok := a.files[cert]
	k, kok := a.files[key]
	if !cok || !kok {
		return nil, fmt.Errorf("missing pair")
	}
	return &SSLInspectResult{CertSHA256: nginxpatch.Hash(c), KeySHA256: nginxpatch.Hash(k), DNSNames: []string{"example.com"}, NotAfter: "2027-01-01", Subject: "example.com", Issuer: "issuer"}, nil
}
func (a *replacementAgent) ApplyTransaction(_ context.Context, req *TransactionRequest) error {
	a.last = req
	if a.fail {
		return fmt.Errorf("uncertain result")
	}
	if !req.TestNginx || !req.ReloadNginx {
		return fmt.Errorf("missing nginx validation")
	}
	for _, c := range req.Changes {
		if c.ExpectedHash == "" || nginxpatch.Hash(a.files[c.Path]) != c.ExpectedHash {
			return fmt.Errorf("missing or wrong hash")
		}
	}
	for _, c := range req.Changes {
		if c.Type == "write" {
			old := a.files[c.Path]
			b, _ := base64.StdEncoding.DecodeString(c.ContentBase64)
			if err := nginxpatch.ValidateMaintenanceWrite(old, string(b), false); err != nil {
				return err
			}
			a.files[c.Path] = string(b)
		}
	}
	return nil
}
func TestCertificateReplacementPreviewApplyDuringMaintenance(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split_http_%t", split), func(t *testing.T) {
			svc, _, database, _ := discoveryService(t)
			svc.cfg.Nginx.PanelDir = "/panel"
			a := &replacementAgent{files: map[string]string{"/config/site.conf": discoveryConfig, "/external/cert": "old certificate", "/external/key": "old private", "/panel/ssl-store/new/cert": "new certificate", "/panel/ssl-store/new/key": "new private"}}
			svc.agent = a
			base := strings.Replace(discoveryConfig, "listen 443 ssl;", "listen 443 ssl; include /panel/rewrite.conf;", 1)
			if split {
				base = `server { listen 80; server_name example.com; return 301 https://$host$request_uri; }` + "\n" + base
			}
			a.files["/config/site.conf"] = base
			a.files["/panel/rewrite.conf"] = ""
			c := &repo.Certificate{ID: "new", Name: "new", DomainsJSON: `["example.com"]`, CertPath: "/panel/ssl-store/new/cert", KeyPath: "/panel/ssl-store/new/key", CertSHA256: nginxpatch.Hash("new certificate"), KeySHA256: nginxpatch.Hash("new private")}
			if err := svc.certRepo.Create(c); err != nil {
				t.Fatal(err)
			}
			if err := svc.sslRepo.Upsert(&repo.SiteSSL{SiteID: "site", Enabled: true, Mode: "existing_files", CertPath: "/external/cert", KeyPath: "/external/key", ForceHTTPS: true, HSTSEnabled: true}); err != nil {
				t.Fatal(err)
			}
			on, err := nginxpatch.SetMaintenance(base, "example.com", nil, "site", "/panel/page.html", true)
			if err != nil {
				t.Fatal(err)
			}
			a.files["/config/site.conf"] = on
			ctx := context.Background()
			preview, err := svc.PreviewReplacement(ctx, "site", &ReplacementRequest{CertificateID: "new"})
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(preview)
			if strings.Contains(string(encoded), "new private") || strings.Contains(string(encoded), c.KeySHA256) {
				t.Fatal("private material or hash exposed in preview")
			}
			req := &ReplacementRequest{DependencyHash: preview.DependencyHash, CertificateID: "new", ExpectedHash: preview.FileHash, CertificateSHA256: preview.CertificateSHA256}
			a.files["/panel/rewrite.conf"] = "# concurrent include change"
			if _, err := svc.ReplaceCertificate(ctx, "site", req, "test"); err == nil {
				t.Fatal("accepted stale include graph")
			}
			a.files["/panel/rewrite.conf"] = ""
			a.files["/config/site.conf"] += "\n# conflict"
			if _, err := svc.ReplaceCertificate(ctx, "site", req, "test"); err == nil {
				t.Fatal("accepted stale preview")
			}
			a.files["/config/site.conf"] = on
			if _, err := database.Exec(`CREATE TRIGGER fail_replace_association BEFORE INSERT ON certificate_sites BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ReplaceCertificate(ctx, "site", req, "test"); err == nil {
				t.Fatal("ignored DB failure after config apply")
			}
			if _, err := database.Exec(`DROP TRIGGER fail_replace_association`); err != nil {
				t.Fatal(err)
			}
			scan, err := svc.ScanCertificate(ctx, "site")
			if err != nil || scan.ReadOnlyReason != "" {
				t.Fatalf("reconciliation scan: %+v %v", scan, err)
			}
			if _, _, err := svc.AdoptCertificate(ctx, &AdoptCertificateRequest{SiteID: "site", ExpectedHash: scan.FileHash}, "reconcile"); err != nil {
				t.Fatal(err)
			}
			meta, err := svc.sslRepo.GetBySiteID("site")
			if err != nil || meta.CertPath != c.CertPath || !meta.ForceHTTPS || !meta.HSTSEnabled || !meta.Enabled || meta.Mode != "existing_files" {
				t.Fatalf("metadata reconciliation changed policy: %+v %v", meta, err)
			}
			if a.files["/external/key"] != "old private" || a.files["/external/cert"] != "old certificate" {
				t.Fatal("external files overwritten")
			}
			if !nginxpatch.HasMaintenance(a.files["/config/site.conf"]) {
				t.Fatal("certificate update removed maintenance")
			}
			if len(a.last.Changes) != 4 || a.last.Changes[0].Type != "check" || a.last.Changes[1].Type != "check" || a.last.Changes[2].Type != "check" {
				t.Fatal("source certificate/key missing expected hash checks")
			}
			off, err := nginxpatch.SetMaintenance(a.files["/config/site.conf"], "example.com", nil, "site", "/panel/page.html", false)
			if err != nil || !strings.Contains(off, c.CertPath) {
				t.Fatal("restore reverted replacement")
			}
			sites, err := repo.NewCertificateRepo(database).AssociatedSites(c.ID)
			if err != nil || len(sites) != 1 {
				t.Fatalf("association missing: %+v %v", sites, err)
			}
		})
	}
}
