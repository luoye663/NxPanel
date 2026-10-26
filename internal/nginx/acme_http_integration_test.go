package nginx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This also runs in the isolated Nginx container, without a public CA or domain.
func TestACMEHTTPWithExpiredCertificate(t *testing.T) {
	binary, err := exec.LookPath("nginx")
	if err != nil {
		t.Skip("nginx binary not installed")
	}
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-48 * time.Hour), NotAfter: time.Now().Add(-24 * time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	write(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	write(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: priv}))
	challengeDir := filepath.Join(dir, ".well-known", "acme-challenge")
	if err := os.MkdirAll(challengeDir, 0700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(challengeDir, "token"), []byte("expected-key-authorization"))
	port := func() int {
		t.Helper()
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return l.Addr().(*net.TCPAddr).Port
	}
	httpPort, httpsPort := port(), port()
	data := &RenderData{RootPath: dir, SSL: &SSLData{Enabled: true, ForceHTTPS: true}}
	config := fmt.Sprintf("pid %s/nginx.pid; error_log stderr; events {} http { access_log off; server { listen 127.0.0.1:%d; listen 127.0.0.1:%d ssl; server_name localhost; ssl_certificate %s; ssl_certificate_key %s;\n%s\n%s\nlocation / { return 204; } } }", dir, httpPort, httpsPort, certPath, keyPath, BuildForceHTTPSBlock(data), BuildACMEChallengeBlock(data))
	configPath := filepath.Join(dir, "nginx.conf")
	write(configPath, []byte(config))
	if out, err := exec.Command(binary, "-p", dir, "-c", configPath, "-t").CombinedOutput(); err != nil {
		t.Fatalf("nginx -t: %v\n%s", err, out)
	}
	cmd := exec.Command(binary, "-p", dir, "-c", configPath, "-g", "daemon off; master_process off;")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Signal(os.Interrupt); cmd.Wait() })
	client := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	base := fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := client.Get(base + "/")
		if err == nil {
			resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/.well-known/acme-challenge/token", 200, "expected-key-authorization"},
		{"/.well-known/acme-challenge/missing", 404, ""},
		{"/", 301, ""},
		{"/.well-known/acme-challenge-other/token", 301, ""},
	} {
		resp, err := client.Get(base + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != tc.status || (tc.body != "" && string(body) != tc.body) {
			t.Fatalf("%s: %d %s", tc.path, resp.StatusCode, body)
		}
	}
	pool := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool.AddCert(parsed)
	_, err = parsed.Verify(x509.VerifyOptions{Roots: pool, DNSName: "127.0.0.1"})
	if err == nil {
		t.Fatal("test certificate must be expired")
	}
}
