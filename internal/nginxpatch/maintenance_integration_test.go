package nginxpatch_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/agent"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

func TestMaintenanceRealRuntime(t *testing.T) {
	images := os.Getenv("NXPANEL_TEST_RUNTIME_IMAGES")
	if images == "" {
		t.Skip("set NXPANEL_TEST_RUNTIME_IMAGES to run Docker Nginx/OpenResty integration")
	}
	probeBinary := filepath.Join(t.TempDir(), "agent-probe.test")
	build := exec.Command("go", "test", "-c", "-o", probeBinary, "./internal/agent")
	build.Dir = filepath.Join("..", "..")
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "CGO_ENABLED=") {
			build.Env = append(build.Env, entry)
		}
	}
	build.Env = append(build.Env, "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native Agent probe: %v\n%s", err, output)
	}
	for _, image := range strings.Split(images, ",") {
		t.Run(image, func(t *testing.T) { testRuntime(t, image, probeBinary) })
	}
}
func testRuntime(t *testing.T, image, probeBinary string) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.com"}, DNSNames: []string{"example.com"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	k, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write("cert.pem", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	write("key.pem", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: k})))
	template.SerialNumber = big.NewInt(2)
	newDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	write("new-cert.pem", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: newDER})))
	write("new-key.pem", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: k})))
	for _, name := range []string{"key.pem", "new-key.pem"} {
		if err := os.Chmod(filepath.Join(dir, name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("challenge", "acme-ok")
	write("page.html", "<h1>Maintenance inherited</h1>")
	sitePath := filepath.Join(dir, "site.conf")
	base := fmt.Sprintf(`server { listen 8080; listen 8443 ssl; server_name example.com;
ssl_certificate %s/cert.pem; ssl_certificate_key %s/key.pem;
error_page 503 /return; recursive_error_pages on;
rewrite ^/rewrite /business last;
if ($uri = /return) { return 200 "original-return"; }
location ^~ /.well-known/acme-challenge/ { auth_basic off; allow all; alias %s/; }
location / { proxy_pass http://127.0.0.1:8081; }
}`, dir, dir, dir)
	write("real-site.conf", base)
	if err := os.Symlink(filepath.Join(dir, "real-site.conf"), sitePath); err != nil {
		t.Fatal(err)
	}
	write("nginx.conf", fmt.Sprintf(`pid %s/nginx.pid; error_log %s/error.log notice; events {} http { access_log off; server { listen 8081; access_log %s/upstream.log; location / { return 200 "upstream-ok"; } } include %s; }`, dir, dir, dir, sitePath))
	bin := "nginx"
	if strings.Contains(image, "openresty") {
		bin = "openresty"
	}
	docker := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("docker %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	container := docker("run", "--rm", "-d", "-p", "127.0.0.1::8080", "-p", "127.0.0.1::8443", "-v", dir+":"+dir, image, bin, "-p", dir+"/", "-c", "nginx.conf", "-g", "daemon off;")
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", container).Run() })
	httpAddr := docker("port", container, "8080/tcp")
	httpsAddr := docker("port", container, "8443/tcp")
	containerProbe := filepath.Join(dir, "agent-probe.test")
	docker("cp", probeBinary, container+":"+containerProbe)
	output := docker("exec", "-e", "NXPANEL_KERNEL_PROBE_BIN="+bin, "-e", "NXPANEL_KERNEL_PROBE_CONF="+filepath.Join(dir, "nginx.conf"), container, containerProbe, "-test.run", "^TestNativeReloadGenerationProbe$", "-test.v")
	t.Log("native process-namespace probe:", output)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DisableKeepAlives: true, TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	t.Cleanup(func() { client.CloseIdleConnections() })
	request := func(scheme, path, method string) (int, string, http.Header) {
		t.Helper()
		addr := httpAddr
		if scheme == "https" {
			addr = httpsAddr
		}
		req, _ := http.NewRequest(method, scheme+"://"+addr+path, strings.NewReader("body"))
		req.Host = "example.com"
		resp, err := client.Do(req)
		if err != nil {
			return 0, err.Error(), nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body), resp.Header
	}
	wait := func(status int) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if code, _, _ := request("http", "/business", "GET"); code == status {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		logs, _ := exec.Command("docker", "logs", container).CombinedOutput()
		t.Fatalf("runtime did not reach HTTP %d: %s", status, logs)
	}
	wait(200)
	wrapper := filepath.Join(dir, "executor.sh")
	write("executor.sh", fmt.Sprintf("#!/bin/sh\nfor arg in \"$@\"; do if [ \"$arg\" = '-s' ] && [ -f '%s/fail-reload' ]; then echo 'injected reload failure' >&2; exit 1; fi; done\nexec docker exec %s %s \"$@\"\n", dir, container, bin))
	if err := os.Chmod(wrapper, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := &app.Config{DataDir: dir, Agent: app.AgentConfig{Token: "test-token"}, Nginx: app.NginxConfig{Bin: wrapper, ConfPath: filepath.Join(dir, "nginx.conf"), PanelDir: filepath.Join(dir, "panel")}}
	srv, err := agent.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.SetPolicyForTest(agent.NewPathPolicy([]string{dir}))
	workers := func() map[string]bool {
		t.Helper()
		active := map[string]bool{}
		for _, line := range strings.Split(docker("top", container, "-eo", "pid,args"), "\n") {
			if strings.Contains(line, "worker process") && !strings.Contains(line, "shutting down") {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					active[fields[0]] = true
				}
			}
		}
		return active
	}
	awaitWorkers := func(ctx context.Context, previousWorkers map[string]bool) error {
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			current := workers()
			oldActive := false
			for pid := range current {
				if previousWorkers[pid] {
					oldActive = true
				}
			}
			if len(current) > 0 && !oldActive {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(30 * time.Millisecond):
			}
		}
	}
	generationConfirmed := false
	probeUnavailable := false
	observationFails := false
	srv.SetReloadProbeForTest(func(context.Context) (func(context.Context) error, error) {
		if probeUnavailable {
			return nil, fmt.Errorf("injected observation setup failure")
		}
		previousWorkers := workers()
		return func(ctx context.Context) error {
			if observationFails {
				return fmt.Errorf("injected observation timeout")
			}
			if err := awaitWorkers(ctx, previousWorkers); err != nil {
				return err
			}
			generationConfirmed = true
			return nil
		}, nil
	})
	apply := func(content string, transition bool) int {
		t.Helper()
		generationConfirmed = false
		previousWorkers := workers()
		old, err := os.ReadFile(sitePath)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(agent.TransactionRequest{OperationID: fmt.Sprintf("op_%d", time.Now().UnixNano()), TestNginx: true, ReloadNginx: true, Changes: []agent.FileChange{{Type: "write", Path: sitePath, ExpectedHash: nginxpatch.Hash(string(old)), MaintenanceTransition: transition, FollowSymlink: true, ContentBase64: base64.StdEncoding.EncodeToString([]byte(content)), Perm: 0644}}})
		req := httptest.NewRequest("POST", "/internal/v1/transactions/apply", bytes.NewReader(body))
		req.Header.Set("X-NxPanel-Agent-Token", "test-token")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Logf("apply rejected: %s", rec.Body.String())
		} else if transition || nginxpatch.HasMaintenance(content) {
			if !generationConfirmed {
				t.Fatal("Agent acknowledged maintenance before observing worker rollover")
			}
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			if err := awaitWorkers(ctx, previousWorkers); err != nil {
				t.Fatal(err)
			}
		}
		return rec.Code
	}
	write("password", "")
	base = strings.Replace(base, "server_name example.com;", fmt.Sprintf(`server_name example.com; auth_basic "restricted"; auth_basic_user_file %s/password; deny all; error_page 404 = @business_error; location @business_error { proxy_pass http://127.0.0.1:8081; }`, dir), 1)
	if code := apply(base, false); code != 200 {
		t.Fatalf("prepare protected original site: %d", code)
	}
	originalStatus, _, _ := request("http", "/business", "GET")
	if originalStatus != 401 && originalStatus != 403 {
		t.Fatalf("original access guard not active: %d", originalStatus)
	}
	on, err := nginxpatch.SetMaintenance(base, "example.com", nil, "site", filepath.Join(dir, "page.html"), true)
	if err != nil {
		t.Fatal(err)
	}
	probeUnavailable = true
	if code := apply(on, true); code != 503 {
		t.Fatalf("unsafe observation setup was accepted: %d", code)
	}
	if actual, _ := os.ReadFile(sitePath); string(actual) != base {
		t.Fatal("wrote configuration without a usable generation observer")
	}
	probeUnavailable = false
	if code := apply(on, true); code != 200 {
		t.Fatalf("enter maintenance: %d", code)
	}
	wait(503)
	before, _ := os.ReadFile(filepath.Join(dir, "upstream.log"))
	for _, scheme := range []string{"http", "https"} {
		for _, path := range []string{"/business", "/rewrite", "/return", "/business?x=1"} {
			for _, method := range []string{"GET", "POST", "HEAD"} {
				code, body, h := request(scheme, path, method)
				if code != 503 || h.Get("Retry-After") != "300" || h.Get("Cache-Control") != "no-store" || (method != "HEAD" && !strings.Contains(body, "Maintenance inherited")) {
					t.Fatalf("%s %s %s: %d headers=%v body=%s", scheme, method, path, code, h, body)
				}
			}
		}
	}
	after, _ := os.ReadFile(filepath.Join(dir, "upstream.log"))
	if string(before) != string(after) {
		t.Fatal("upstream received new requests during maintenance")
	}
	for _, scheme := range []string{"http", "https"} {
		code, body, _ := request(scheme, "/.well-known/acme-challenge/challenge", "GET")
		if code != 200 || body != "acme-ok" {
			t.Fatalf("ACME %s: %d %s", scheme, code, body)
		}
	}
	for _, scheme := range []string{"http", "https"} {
		code, _, _ := request(scheme, "/.well-known/acme-challenge/missing-token", "GET")
		if code != 404 {
			t.Fatalf("missing ACME token bypassed maintenance: %d", code)
		}
	}
	if actual, _ := os.ReadFile(filepath.Join(dir, "upstream.log")); string(actual) != string(before) {
		t.Fatal("ACME error fallback reached business upstream")
	}
	write("page.html", "<h1>Site override</h1>")
	if code, body, _ := request("https", "/", "GET"); code != 503 || !strings.Contains(body, "Site override") {
		t.Fatalf("override: %d %s", code, body)
	}
	if err := os.Remove(filepath.Join(dir, "page.html")); err != nil {
		t.Fatal(err)
	}
	if code, _, headers := request("https", "/missing-page", "GET"); code != 503 || headers.Get("Cache-Control") != "no-store" {
		t.Fatalf("missing maintenance page must fail closed with 503: %d %v", code, headers)
	}
	write("page.html", "<h1>Site override</h1>")
	bad := strings.Replace(on, dir+"/cert.pem", dir+"/missing.pem", 1)
	if code := apply(bad, false); code == 200 {
		t.Fatal("nginx -t accepted missing certificate")
	}
	if actual, _ := os.ReadFile(sitePath); string(actual) != on {
		t.Fatal("nginx -t failure did not restore current config")
	}
	updated, err := nginxpatch.ReplaceCertificate(on, "example.com", nginxpatch.Hash(on), dir+"/new-cert.pem", dir+"/new-key.pem")
	if err != nil {
		t.Fatal(err)
	}
	write("fail-reload", "")
	if code := apply(updated, false); code == 200 {
		t.Fatal("reload failure accepted")
	}
	if actual, _ := os.ReadFile(sitePath); string(actual) != on {
		t.Fatal("reload failure did not restore current config")
	}
	if err := os.Remove(filepath.Join(dir, "fail-reload")); err != nil {
		t.Fatal(err)
	}
	observationFails = true
	if code := apply(updated, false); code != 503 {
		t.Fatalf("unconfirmed worker generation was acknowledged: %d", code)
	}
	if actual, _ := os.ReadFile(sitePath); string(actual) != updated {
		t.Fatal("unknown reload outcome blindly reverted desired configuration")
	}
	observationFails = false
	if code := apply(updated, false); code != 200 {
		t.Fatalf("certificate update during maintenance: %d", code)
	}
	off, err := nginxpatch.SetMaintenance(updated, "example.com", nil, "site", filepath.Join(dir, "page.html"), false)
	if err != nil {
		t.Fatal(err)
	}
	if code := apply(off, true); code != 200 {
		t.Fatalf("restore: %d", code)
	}
	wait(originalStatus)
	if actual, _ := os.ReadFile(sitePath); !strings.Contains(string(actual), "new-cert.pem") || nginxpatch.HasMaintenance(string(actual)) {
		t.Fatal("restore reverted certificate or kept maintenance")
	}
	if info, err := os.Lstat(sitePath); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("maintenance replaced the enabled-site symlink")
	}
	peer, err := client.Get("https://" + httpsAddr + "/return")
	if err != nil {
		t.Fatal(err)
	}
	_ = peer.Body.Close()
	if peer.TLS == nil || len(peer.TLS.PeerCertificates) == 0 || peer.TLS.PeerCertificates[0].SerialNumber.Cmp(big.NewInt(2)) != 0 {
		t.Fatal("HTTPS handshake reverted the replacement certificate after restore")
	}
	if code, body, _ := request("https", "/return", "GET"); code != 200 || body != "original-return" {
		t.Fatalf("original return not restored: %d %s", code, body)
	}
	unconditional := strings.Replace(off, "rewrite ^/rewrite", `return 200 "unconditional"; rewrite ^/rewrite`, 1)
	if code := apply(unconditional, false); code != 200 {
		t.Fatalf("prepare server return: %d", code)
	}
	withReturn, err := nginxpatch.SetMaintenance(unconditional, "example.com", nil, "site", filepath.Join(dir, "page.html"), true)
	if err != nil {
		t.Fatal(err)
	}
	if code := apply(withReturn, true); code != 200 {
		t.Fatalf("maintenance over unconditional return: %d", code)
	}
	wait(503)
	for _, scheme := range []string{"http", "https"} {
		code, body, _ := request(scheme, "/business", "POST")
		if code != 503 || !strings.Contains(body, "Site override") {
			t.Fatalf("server return bypassed maintenance: %d %s", code, body)
		}
	}
	unlock, err := nginxpatch.SetMaintenance(withReturn, "example.com", nil, "site", filepath.Join(dir, "page.html"), false)
	if err != nil {
		t.Fatal(err)
	}
	if code := apply(unlock, true); code != 200 {
		t.Fatalf("unlock: %d", code)
	}
	noACME := strings.Replace(unlock, fmt.Sprintf("location ^~ /.well-known/acme-challenge/ { auth_basic off; allow all; alias %s/; }", dir), "", 1)
	if code := apply(noACME, false); code != 200 {
		t.Fatalf("prepare no ACME route: %d", code)
	}
	noACMEOn, err := nginxpatch.SetMaintenance(noACME, "example.com", nil, "site", filepath.Join(dir, "page.html"), true)
	if err != nil {
		t.Fatal(err)
	}
	if code := apply(noACMEOn, true); code != 200 {
		t.Fatalf("no ACME maintenance: %d", code)
	}
	wait(503)
	for _, scheme := range []string{"http", "https"} {
		if code, _, _ := request(scheme, "/.well-known/acme-challenge/not-a-business-bypass", "GET"); code != 404 {
			t.Fatalf("ACME without prior route reached business logic: %d", code)
		}
	}
}
