package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/scheduledtask"
	"github.com/luoye663/nxpanel/internal/sse"
	"github.com/luoye663/nxpanel/internal/ssl"
)

type renewalAgent struct {
	content      []byte
	transactions int
	applyErr     error
	cleanupErr   error
	removed      bool
}

func (a *renewalAgent) ReadFile(context.Context, string) ([]byte, string, error) {
	return a.content, "", nil
}
func (a *renewalAgent) ApplyTransaction(_ context.Context, req *ssl.TransactionRequest) error {
	a.transactions++
	if !req.TestNginx || !req.ReloadNginx {
		return errors.New("missing test/reload")
	}
	if a.applyErr != nil {
		return a.applyErr
	}
	a.content, _ = base64.StdEncoding.DecodeString(req.Changes[0].ContentBase64)
	return nil
}
func (*renewalAgent) FilesWrite(context.Context, string, string) error  { return nil }
func (*renewalAgent) FilesMkdir(context.Context, string) error          { return nil }
func (*renewalAgent) FilesRead(context.Context, string) ([]byte, error) { return nil, nil }
func (a *renewalAgent) FilesRemove(ctx context.Context, _ []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.removed = true
	return a.cleanupErr
}

type renewalDeployer struct {
	err error
	req *ssl.DeployFromStoreRequest
}

func (d *renewalDeployer) DeployFromStore(_ context.Context, _ string, req *ssl.DeployFromStoreRequest, _ string) (*ssl.SSLResponse, string, error) {
	d.req = req
	return nil, "", d.err
}

const oldChallengeConfig = `server {
#NXPANEL-DOCUMENT-START
#NXPANEL-DOCUMENT-END
#NXPANEL-FORCE-HTTPS-START
if ($scheme = http) { return 301 https://$host$request_uri; }
#NXPANEL-FORCE-HTTPS-END
location /custom { return 204; }
}`

func TestPrepareChallengeConfig(t *testing.T) {
	if err := nginx.InitTemplates("../../configs/templates"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, content string
		fail          bool
	}{
		{"old", oldChallengeConfig, false},
		{"no_redirect", strings.ReplaceAll(oldChallengeConfig, "#NXPANEL-FORCE-HTTPS-START\nif ($scheme = http) { return 301 https://$host$request_uri; }\n#NXPANEL-FORCE-HTTPS-END\n", ""), false},
		{"broken", strings.ReplaceAll(oldChallengeConfig, "#NXPANEL-FORCE-HTTPS-END", ""), true},
		{"duplicate", oldChallengeConfig + "\n#NXPANEL-FORCE-HTTPS-START", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newAdmissionTestService(t, context.Background(), 2)
			agent := &renewalAgent{content: []byte(tc.content)}
			svc.agent = agent
			site, _ := svc.siteRepo.GetByID("site-1")
			err := svc.prepareChallengeConfig(context.Background(), site, "test")
			if tc.fail {
				if err == nil || agent.transactions != 0 {
					t.Fatalf("unsafe patch: %v, %d", err, agent.transactions)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := string(agent.content)
			if !strings.Contains(got, "location ^~ /.well-known/acme-challenge/") || !strings.Contains(got, "location /custom { return 204; }") {
				t.Fatal(got)
			}
			if strings.Contains(tc.content, "FORCE-HTTPS") && !strings.Contains(got, "$nxpanel_https_redirect") {
				t.Fatal(got)
			}
			if err := svc.prepareChallengeConfig(context.Background(), site, "test"); err != nil {
				t.Fatal(err)
			}
			if agent.transactions != 1 {
				t.Fatalf("not idempotent: %d", agent.transactions)
			}
		})
	}
}

func TestPrepareFailureStopsBeforeCA(t *testing.T) {
	if err := nginx.InitTemplates("../../configs/templates"); err != nil {
		t.Fatal(err)
	}
	svc := newAdmissionTestService(t, context.Background(), 1)
	agent := &renewalAgent{content: []byte(oldChallengeConfig), applyErr: errors.New("nginx test failed")}
	svc.agent = agent
	done := make(chan error, 1)
	id, err := svc.applyCertificate(context.Background(), &ApplyRequest{SiteID: "site-1", Domains: []string{"example.com"}, Email: "a@example.com"}, "test", applyOptions{done: done})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("expected failure")
	}
	order, _ := svc.acmeRepo.GetOrderByID(id)
	if order.Status != "failed" || order.ErrorType != "config_update_failed" {
		t.Fatalf("%+v", order)
	}
	if string(agent.content) != oldChallengeConfig {
		t.Fatal("changed on failure")
	}
}

func TestPreValidationDoesNotFollowRedirect(t *testing.T) {
	reached := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusMovedPermanently)
	}))
	defer server.Close()
	resp, err := newPreValidationHTTPClient().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if reached || resp.StatusCode != 301 || !strings.Contains(validationStatusError(resp).Error(), target.URL) {
		t.Fatal("redirect was not diagnosed")
	}
}

func TestCleanupAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	agent := &renewalAgent{}
	p := &http01Provider{ctx: ctx, agent: agent, writtenFiles: []string{"token"}}
	if err := p.cleanupFiles(); err != nil {
		t.Fatal(err)
	}
	if !agent.removed || len(p.writtenFiles) != 0 {
		t.Fatal("cleanup failed")
	}
}

func TestSiteAdmissionAndTaskCancellation(t *testing.T) {
	svc := newAdmissionTestService(t, context.Background(), 2)
	started := make(chan struct{})
	svc.applyJob = func(ctx context.Context, _ string, _ string, _ []string, _ string, _ string, _ string, _ bool) {
		close(started)
		<-ctx.Done()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	req := &ApplyRequest{SiteID: "site-1", Domains: []string{"example.com"}, Email: "a@example.com"}
	if _, err := svc.applyCertificate(ctx, req, "test", applyOptions{done: done, taskCtx: ctx}); err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := svc.ApplyCertificate(context.Background(), req, "test"); !isBusy(err) {
		t.Fatalf("same site admitted: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not cancel")
	}
	if !svc.admitSite("site-1") {
		t.Fatal("site slot leaked")
	}
	svc.releaseSite("site-1")
}

func testCertificate(t *testing.T) *certificate.Resource {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.com"}, DNSNames: []string{"example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return &certificate.Resource{Certificate: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: priv})}
}

func TestSaveAndDeployResult(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "deployment_failed"}[failed], func(t *testing.T) {
			svc := newAdmissionTestService(t, context.Background(), 1)
			deployer := &renewalDeployer{}
			if failed {
				deployer.err = errors.New("reload failed")
			}
			svc.sslDeployer = deployer
			if err := svc.sslRepo.Upsert(&repo.SiteSSL{SiteID: "site-1", Enabled: true, Mode: "existing_files", ForceHTTPS: false, HSTSEnabled: true}); err != nil {
				t.Fatal(err)
			}
			if err := svc.acmeRepo.CreateOrder(&repo.ACMEOrder{ID: "order", SiteID: "site-1", DomainsJSON: `["example.com"]`, Status: "verifying"}); err != nil {
				t.Fatal(err)
			}
			if err := svc.opRepo.Create(&repo.Operation{ID: "op", Status: "pending", Action: "acme.obtain", TargetType: "site", TargetID: "site-1", Actor: "admin"}); err != nil {
				t.Fatal(err)
			}
			stream := svc.sseHub.CreateStream("test")
			err := svc.saveAndDeploy(context.Background(), "order", "site-1", "test", "op", "/cert", "/key", "example.com", testCertificate(t), stream, sse.NewLogWriter(stream, "|-"))
			if (err != nil) != failed {
				t.Fatalf("result: %v", err)
			}
			order, _ := svc.acmeRepo.GetOrderByID("order")
			if order.Status != "success" || order.CertificateID == "" {
				t.Fatalf("certificate lost: %+v", order)
			}
			if (order.ErrorType == "deployment_failed") != failed {
				t.Fatalf("%+v", order)
			}
			if deployer.req.ForceHTTPS || !deployer.req.HSTSEnabled {
				t.Fatal("settings changed")
			}
		})
	}
}

func TestScheduledRenewalWaitsForCompletion(t *testing.T) {
	svc := newAdmissionTestService(t, context.Background(), 1)
	expiry := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := svc.acmeRepo.CreateOrder(&repo.ACMEOrder{ID: "old", SiteID: "site-1", DomainsJSON: `["example.com"]`, Email: "a@example.com", Status: "success", AutoRenew: true, ExpiresAt: &expiry}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	svc.applyJob = func(ctx context.Context, id string, _ string, _ []string, _ string, _ string, _ string, _ bool) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		svc.acmeRepo.UpdateOrderSuccess(id, "", time.Now().Add(90*24*time.Hour).UTC().Format(time.RFC3339))
	}
	done := make(chan error, 1)
	go func() {
		summary, err := svc.RunScheduledRenewal(context.Background(), ACMERenewalParams{RenewBeforeDays: 30}, scheduledtask.RunContext{})
		if err == nil && summary.Success != 1 {
			err = errors.New("wrong success count")
		}
		done <- err
	}()
	<-started
	select {
	case <-done:
		t.Fatal("reported success before completion")
	default:
	}
	old, _ := svc.acmeRepo.GetOrderByID("old")
	if old.LastRenewedAt != nil || !old.AutoRenew {
		t.Fatal("old order retired too soon")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	old, _ = svc.acmeRepo.GetOrderByID("old")
	if old.LastRenewedAt == nil || old.AutoRenew {
		t.Fatal("old order not retired")
	}
}

func TestRenewalCandidateSelection(t *testing.T) {
	for _, tc := range []struct {
		name, status, errType    string
		enabled, future, wantOld bool
	}{
		{"new_valid", "success", "", true, true, false},
		{"disabled", "success", "", false, false, false},
		{"new_failed", "failed", "challenge_failed", true, false, true},
		{"deploy_failed", "success", "deployment_failed", true, true, true},
		{"deploy_interrupted", "success", "deployment_pending", true, true, true},
		{"duplicate_expired", "success", "", true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newAdmissionTestService(t, context.Background(), 1)
			expiry := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
			old := &repo.ACMEOrder{ID: "old", SiteID: "site-1", DomainsJSON: `["EXAMPLE.com.", "www.example.com"]`, Status: "success", AutoRenew: true, ExpiresAt: &expiry}
			if err := svc.acmeRepo.CreateOrder(old); err != nil {
				t.Fatal(err)
			}
			newerExpiry := expiry
			if tc.future {
				newerExpiry = time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)
			}
			if err := svc.acmeRepo.CreateOrder(&repo.ACMEOrder{ID: "new", SiteID: "site-1", DomainsJSON: `["www.example.com","example.com","example.com"]`, Status: tc.status, ErrorType: tc.errType, AutoRenew: tc.enabled, ExpiresAt: &newerExpiry}); err != nil {
				t.Fatal(err)
			}
			orders, err := svc.acmeRepo.ListExpiringOrders(time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339))
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tc.wantOld {
				want = "old"
			} else if tc.enabled && !tc.future {
				want = "new"
			}
			if want == "" {
				if len(orders) != 0 {
					t.Fatalf("unexpected candidates: %+v", orders)
				}
			} else if len(orders) != 1 || orders[0].ID != want {
				t.Fatalf("want %s: %+v", want, orders)
			}
		})
	}
}

func TestScheduledFailureRetainsOldOrder(t *testing.T) {
	svc := newAdmissionTestService(t, context.Background(), 1)
	svc.agent = &renewalAgent{content: []byte(oldChallengeConfig), applyErr: errors.New("reload failed")}
	if err := nginx.InitTemplates("../../configs/templates"); err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	if err := svc.acmeRepo.CreateOrder(&repo.ACMEOrder{ID: "old", SiteID: "site-1", DomainsJSON: `["example.com"]`, Email: "a@example.com", Status: "success", AutoRenew: true, ExpiresAt: &expiry}); err != nil {
		t.Fatal(err)
	}
	summary, err := svc.RunScheduledRenewal(context.Background(), ACMERenewalParams{RenewBeforeDays: 30}, scheduledtask.RunContext{})
	if err == nil || summary.Success != 0 || summary.Failed != 1 {
		t.Fatalf("%+v %v", summary, err)
	}
	old, _ := svc.acmeRepo.GetOrderByID("old")
	if !old.AutoRenew || old.LastRenewedAt != nil {
		t.Fatal("failed renewal retired old order")
	}
}

func TestHTTPContentCheck(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"valid", "key-auth", 200, false},
		{"mismatch", "wrong", 200, true},
		{"missing", "", 404, true},
		{"redirect", "", 301, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != "example.com" {
					t.Errorf("Host = %s", r.Host)
				}
				w.Header().Set("Location", "https://example.com/")
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			if err := httpContentCheck(context.Background(), server.URL, "example.com", "key-auth"); (err != nil) != tc.wantErr {
				t.Fatalf("error: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := httpContentCheck(ctx, "http://127.0.0.1/", "example.com", "key-auth"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := httpContentCheck(ctx, server.URL, "example.com", "key-auth"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

func TestRenewalCompletionRollsBack(t *testing.T) {
	svc := newAdmissionTestService(t, context.Background(), 1)
	for _, id := range []string{"old", "new"} {
		if err := svc.acmeRepo.CreateOrder(&repo.ACMEOrder{ID: id, SiteID: "site-1", DomainsJSON: `["example.com"]`, Status: "pending", AutoRenew: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.acmeRepo.CompleteRenewal("old", "new"); err == nil {
		t.Fatal("accepted unfinished new order")
	}
	old, _ := svc.acmeRepo.GetOrderByID("old")
	if !old.AutoRenew || old.LastRenewedAt != nil {
		t.Fatal("old order changed")
	}
	if err := svc.acmeRepo.UpdateOrderStatus("new", "success", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.acmeRepo.UpdateOrderAutoRenew("old", false); err != nil {
		t.Fatal(err)
	}
	if err := svc.acmeRepo.CompleteRenewal("old", "new"); err != nil {
		t.Fatal(err)
	}
	newer, _ := svc.acmeRepo.GetOrderByID("new")
	if newer.AutoRenew {
		t.Fatal("auto-renew preference not inherited")
	}
}

func TestInvalidCertificateDoesNotDeploy(t *testing.T) {
	svc := newAdmissionTestService(t, context.Background(), 1)
	deployer := &renewalDeployer{}
	svc.sslDeployer = deployer
	if err := svc.acmeRepo.CreateOrder(&repo.ACMEOrder{ID: "bad", SiteID: "site-1", DomainsJSON: `["example.com"]`, Status: "verifying"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.opRepo.Create(&repo.Operation{ID: "op", Status: "pending", Action: "acme.obtain", TargetType: "site", TargetID: "site-1", Actor: "admin"}); err != nil {
		t.Fatal(err)
	}
	stream := svc.sseHub.CreateStream("test")
	err := svc.saveAndDeploy(context.Background(), "bad", "site-1", "test", "op", "/cert", "/key", "example.com", &certificate.Resource{}, stream, sse.NewLogWriter(stream, "|-"))
	if err == nil || deployer.req != nil {
		t.Fatal("invalid certificate was deployed")
	}
	order, _ := svc.acmeRepo.GetOrderByID("bad")
	if order.Status != "failed" || order.ErrorType != "certificate_invalid" {
		t.Fatalf("%+v", order)
	}
}
