package app

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestGenerateSelfSignedCert(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.crt")
	keyPath := filepath.Join(tmpDir, "test.key")

	if err := GenerateSelfSignedCert(certPath, keyPath, 24*time.Hour, nil); err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}

	certInfo, err := os.Stat(certPath)
	if err != nil {
		t.Fatalf("cert file stat: %v", err)
	}
	if certInfo.Mode().Perm() != 0644 {
		t.Errorf("cert permissions = %o, want 0644", certInfo.Mode().Perm())
	}

	keyInfo, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("key file stat: %v", err)
	}
	if keyInfo.Mode().Perm() != 0600 {
		t.Errorf("key permissions = %o, want 0600", keyInfo.Mode().Perm())
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode cert PEM failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	if !uuidPattern.MatchString(cert.Subject.CommonName) {
		t.Errorf("CommonName = %q, want UUID v4 format", cert.Subject.CommonName)
	}

	foundDNS := false
	for _, dns := range cert.DNSNames {
		if dns == "localhost" {
			foundDNS = true
		}
	}
	if !foundDNS {
		t.Error("DNSNames missing localhost")
	}

	foundIP := false
	for _, ip := range cert.IPAddresses {
		if ip.String() == "127.0.0.1" {
			foundIP = true
		}
	}
	if !foundIP {
		t.Error("IPAddresses missing 127.0.0.1")
	}

	if cert.NotAfter.Sub(cert.NotBefore) < 23*time.Hour {
		t.Errorf("validity too short: %v", cert.NotAfter.Sub(cert.NotBefore))
	}
}

func TestGenerateSelfSignedCertNoLeakedIPs(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.crt")
	keyPath := filepath.Join(tmpDir, "test.key")

	if err := GenerateSelfSignedCert(certPath, keyPath, 24*time.Hour, nil); err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode cert PEM failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	for _, ip := range cert.IPAddresses {
		if !ip.IsLoopback() {
			t.Errorf("non-loopback IP found in SAN: %s", ip)
		}
	}
}

func TestGenerateSelfSignedCertExtraSANs(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "test.crt")
	keyPath := filepath.Join(tmpDir, "test.key")

	extraSANs := []string{"1.2.3.4", "panel.example.com", "  ", "::1"}
	if err := GenerateSelfSignedCert(certPath, keyPath, 24*time.Hour, extraSANs); err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		t.Fatal("decode cert PEM failed")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	foundIP := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("1.2.3.4")) {
			foundIP = true
		}
	}
	if !foundIP {
		t.Error("IPAddresses missing configured 1.2.3.4")
	}

	foundDNS := false
	for _, dns := range cert.DNSNames {
		if dns == "panel.example.com" {
			foundDNS = true
		}
	}
	if !foundDNS {
		t.Error("DNSNames missing configured panel.example.com")
	}

	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("10.0.0.1")) {
			t.Error("unexpected IP in SAN")
		}
	}
}

func TestGenerateSelfSignedCertCreatesParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	certPath := filepath.Join(tmpDir, "nested", "dir", "test.crt")
	keyPath := filepath.Join(tmpDir, "nested", "dir", "test.key")

	if err := GenerateSelfSignedCert(certPath, keyPath, time.Hour, nil); err != nil {
		t.Fatalf("GenerateSelfSignedCert: %v", err)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert not created: %v", err)
	}
}

func TestIsCertExpiredOrMissing(t *testing.T) {
	tmpDir := t.TempDir()

	if !IsCertExpiredOrMissing(filepath.Join(tmpDir, "nonexistent.crt")) {
		t.Error("missing cert should be expired")
	}

	certPath := filepath.Join(tmpDir, "valid.crt")
	keyPath := filepath.Join(tmpDir, "valid.key")
	if err := GenerateSelfSignedCert(certPath, keyPath, 48*time.Hour, nil); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if IsCertExpiredOrMissing(certPath) {
		t.Error("valid cert should not be expired")
	}

	shortCertPath := filepath.Join(tmpDir, "short.crt")
	shortKeyPath := filepath.Join(tmpDir, "short.key")
	if err := GenerateSelfSignedCert(shortCertPath, shortKeyPath, 12*time.Hour, nil); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !IsCertExpiredOrMissing(shortCertPath) {
		t.Error("cert with <24h validity should be expired")
	}
}

func TestEnsureAPICertificateAutoGenerate(t *testing.T) {
	tmpDir := t.TempDir()
	tlsCfg := &TLSConfig{}

	certPath, keyPath, err := EnsureAPICertificate(tlsCfg, tmpDir)
	if err != nil {
		t.Fatalf("EnsureAPICertificate: %v", err)
	}

	expectedCert := filepath.Join(tmpDir, "tls", "api.crt")
	expectedKey := filepath.Join(tmpDir, "tls", "api.key")
	if certPath != expectedCert {
		t.Errorf("certPath = %q, want %q", certPath, expectedCert)
	}
	if keyPath != expectedKey {
		t.Errorf("keyPath = %q, want %q", keyPath, expectedKey)
	}
	if _, err := os.Stat(certPath); err != nil {
		t.Errorf("cert not created: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Errorf("key not created: %v", err)
	}
}

func TestEnsureAPICertificateIdempotent(t *testing.T) {
	tmpDir := t.TempDir()
	tlsCfg := &TLSConfig{}

	certPath1, keyPath1, err := EnsureAPICertificate(tlsCfg, tmpDir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	origCert, err := os.ReadFile(certPath1)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}

	certPath2, keyPath2, err := EnsureAPICertificate(tlsCfg, tmpDir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if certPath2 != certPath1 || keyPath2 != keyPath1 {
		t.Error("second call returned different paths")
	}

	newCert, err := os.ReadFile(certPath2)
	if err != nil {
		t.Fatalf("read cert second time: %v", err)
	}
	if string(origCert) != string(newCert) {
		t.Error("cert was regenerated despite being valid")
	}
}

func TestEnsureAPICertificateExplicitPaths(t *testing.T) {
	tmpDir := t.TempDir()
	explicitCert := filepath.Join(tmpDir, "explicit.crt")
	explicitKey := filepath.Join(tmpDir, "explicit.key")
	tlsCfg := &TLSConfig{Cert: explicitCert, Key: explicitKey}

	certPath, keyPath, err := EnsureAPICertificate(tlsCfg, tmpDir)
	if err != nil {
		t.Fatalf("EnsureAPICertificate: %v", err)
	}
	if certPath != explicitCert || keyPath != explicitKey {
		t.Errorf("paths = %q, %q; want %q, %q", certPath, keyPath, explicitCert, explicitKey)
	}
}

func TestEnsureAPICertificateCustomValidity(t *testing.T) {
	tmpDir := t.TempDir()
	tlsCfg := &TLSConfig{CertValidity: "48h"}

	certPath, _, err := EnsureAPICertificate(tlsCfg, tmpDir)
	if err != nil {
		t.Fatalf("EnsureAPICertificate: %v", err)
	}

	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(data)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	validity := cert.NotAfter.Sub(cert.NotBefore)
	if validity < 47*time.Hour || validity > 49*time.Hour {
		t.Errorf("validity = %v, want ~48h", validity)
	}
}

func TestEnsureAPICertificateWithSANs(t *testing.T) {
	tmpDir := t.TempDir()
	tlsCfg := &TLSConfig{
		SANs: []string{"203.0.113.50", "panel.example.com"},
	}

	certPath, _, err := EnsureAPICertificate(tlsCfg, tmpDir)
	if err != nil {
		t.Fatalf("EnsureAPICertificate: %v", err)
	}

	data, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	block, _ := pem.Decode(data)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}

	foundIP := false
	for _, ip := range cert.IPAddresses {
		if ip.Equal(net.ParseIP("203.0.113.50")) {
			foundIP = true
		}
	}
	if !foundIP {
		t.Error("cert missing configured SAN IP 203.0.113.50")
	}

	foundDNS := false
	for _, dns := range cert.DNSNames {
		if dns == "panel.example.com" {
			foundDNS = true
		}
	}
	if !foundDNS {
		t.Error("cert missing configured SAN DNS panel.example.com")
	}
}

func TestNewAPITLSConfig(t *testing.T) {
	cfg := NewAPITLSConfig()
	if cfg == nil {
		t.Fatal("NewAPITLSConfig returned nil")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want TLS 1.2", cfg.MinVersion)
	}
}

func TestGenerateUUID(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		u, err := generateUUID()
		if err != nil {
			t.Fatalf("generateUUID: %v", err)
		}
		if !uuidPattern.MatchString(u) {
			t.Errorf("UUID %q does not match v4 format", u)
		}
		if seen[u] {
			t.Errorf("duplicate UUID: %s", u)
		}
		seen[u] = true
	}
}
