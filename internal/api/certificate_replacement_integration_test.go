package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/luoye663/nxpanel/internal/agent"
	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
	"github.com/luoye663/nxpanel/internal/ssl"
)

// Exercise the production adapter and Agent, not a service mock that can hide
// dropped fields. Only the nginx executable is replaced; all files are temporary.
func TestCertificateReplacementThroughUnixAgent(t *testing.T) {
	for _, failTest := range []bool{false, true} {
		t.Run(fmt.Sprintf("nginx_test_failure_%t", failTest), func(t *testing.T) {
			root := t.TempDir()
			write := func(path string, data []byte, perm os.FileMode) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, perm); err != nil {
					t.Fatal(err)
				}
			}
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "www.example.com"}, DNSNames: []string{"www.example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			keyDER, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
			keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
			certPath, keyPath := filepath.Join(root, "ssl-store/new/cert.pem"), filepath.Join(root, "ssl-store/new/key.pem")
			oldCert, oldKey := filepath.Join(root, "external/cert.pem"), filepath.Join(root, "external/key.pem")
			for _, path := range []string{certPath, oldCert} {
				write(path, certPEM, 0644)
			}
			for _, path := range []string{keyPath, oldKey} {
				write(path, keyPEM, 0600)
			}
			confPath := filepath.Join(root, "site.conf")
			original := fmt.Sprintf("# keep custom content\nserver { listen 443 ssl; server_name example.com www.example.com; ssl_certificate %s; ssl_certificate_key %s; location / { return 200 ok; } }\n", oldCert, oldKey)
			write(confPath, []byte(original), 0644)
			bin, commands := filepath.Join(root, "nginx"), filepath.Join(root, "commands")
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> '%s'\ncase \"$1\" in\n-T) cat '%s';;\n-t) %s;;\nesac\n", commands, confPath, "exit 0")
			if failTest {
				script = strings.Replace(script, "-t) exit 0", "-t) echo test-failed >&2; exit 1", 1)
			}
			write(bin, []byte(script), 0700)
			socket := filepath.Join(root, "agent.sock")
			cfg := &app.Config{DataDir: root, Agent: app.AgentConfig{Token: "test-token", SocketPath: socket}, Nginx: app.NginxConfig{PanelDir: root, Bin: bin, ConfPath: confPath}}
			agentServer, err := agent.NewServer(cfg)
			if err != nil {
				t.Fatal(err)
			}
			agentServer.SetPolicyForTest(agent.NewPathPolicy([]string{root}))
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			httpServer := &http.Server{Handler: agentServer.Handler()}
			go func() { _ = httpServer.Serve(listener) }()
			t.Cleanup(func() { _ = httpServer.Close() })
			client := agentclient.NewWithDefaults(socket, cfg.Agent.Token)
			t.Cleanup(func() { _ = client.Close() })
			adapter := &sslAgentAdapter{client: client}
			inspected, err := adapter.SSLInspectFiles(context.Background(), certPath, keyPath)
			if err != nil || inspected.KeySHA256 != nginxpatch.Hash(string(keyPEM)) {
				t.Fatalf("lost key hash in adapter: %+v %v", inspected, err)
			}
			database := newTestDB(t)
			sites, store := repo.NewSiteRepo(database), repo.NewCertificateRepo(database)
			if err := sites.Create(&repo.Site{ID: "site", PrimaryDomain: "example.com", DomainsJSON: `["example.com","www.example.com"]`, BindingsJSON: `[]`, Status: "enabled", HTTPPort: 80, HTTPSPort: 443, ConfigPath: confPath, EnabledPath: confPath, RootPath: root}); err != nil {
				t.Fatal(err)
			}
			if err := store.Create(&repo.Certificate{ID: "new", Name: "new", DomainsJSON: `["www.example.com"]`, CertPath: certPath, KeyPath: keyPath, CertSHA256: inspected.CertSHA256}); err != nil {
				t.Fatal(err)
			}
			service := ssl.NewService(sites, repo.NewSSLRepo(database), store, repo.NewOperationRepo(database), adapter, cfg)
			server := &Server{sslSvc: service}
			router := chi.NewRouter()
			router.Post("/sites/{site_id}/certificates/replace-preview", server.handleCertificateReplacePreview)
			router.Post("/sites/{site_id}/certificates/replace", server.handleCertificateReplace)
			request := func(path string, body any) *httptest.ResponseRecorder {
				t.Helper()
				data, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequest("POST", "/sites/site/certificates/"+path, bytes.NewReader(data)))
				if strings.Contains(rec.Body.String(), "PRIVATE KEY") || strings.Contains(rec.Body.String(), inspected.KeySHA256) {
					t.Fatal("private material exposed")
				}
				return rec
			}
			rec := request("replace-preview", ssl.ReplacementRequest{CertificateID: "new"})
			if rec.Code != 200 {
				t.Fatal(rec.Body.String())
			}
			var result struct {
				Data ssl.ReplacementPreview `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			preview := result.Data
			if len(preview.UnmatchedDomains) != 1 || preview.UnmatchedDomains[0] != "example.com" {
				t.Fatalf("expected partial coverage: %+v", preview)
			}
			rec = request("replace", ssl.ReplacementRequest{CertificateID: "new", ExpectedHash: preview.FileHash, CertificateSHA256: preview.CertificateSHA256, DependencyHash: preview.DependencyHash})
			content, err := os.ReadFile(confPath)
			if err != nil {
				t.Fatal(err)
			}
			if failTest {
				if rec.Code == 200 || string(content) != original {
					t.Fatalf("nginx test failure did not preserve config: %s", rec.Body.String())
				}
			} else {
				if rec.Code != 200 {
					t.Fatal(rec.Body.String())
				}
				expected := strings.ReplaceAll(strings.ReplaceAll(original, oldCert, certPath), oldKey, keyPath)
				if string(content) != expected {
					t.Fatal("replacement changed unrelated configuration")
				}
				scan, err := service.ScanCertificate(context.Background(), "site")
				if err != nil || scan.AdoptionStatus != "adopted" || scan.CertificateID != "new" {
					t.Fatalf("post-apply status: %+v %v", scan, err)
				}
			}
			for path, expected := range map[string][]byte{oldCert: certPEM, oldKey: keyPEM} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, expected) {
					t.Fatal("external files changed")
				}
			}
			calls, err := os.ReadFile(commands)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(calls), "-t -c "+confPath) || (!failTest && !strings.Contains(string(calls), "-s reload")) || (failTest && strings.Contains(string(calls), "-s reload")) {
				t.Fatalf("unexpected nginx commands: %s", calls)
			}
		})
	}
}
