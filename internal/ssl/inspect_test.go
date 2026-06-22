// ssl 包测试 — 证书解析与校验
package ssl

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

// generateTestCert 生成用于测试的自签名证书和私钥
func generateTestCert(t *testing.T, notBefore, notAfter time.Time) (certPEM, keyPEM []byte) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥失败: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "test.example.com",
		},
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		DNSNames:    []string{"test.example.com", "www.test.example.com"},
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("创建证书失败: %v", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("序列化私钥失败: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM
}

func TestInspectPair_Valid(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t,
		time.Now().Add(-24*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)

	info, err := InspectPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("InspectPair 失败: %v", err)
	}

	if info.Subject == "" {
		t.Error("Subject 不应为空")
	}
	if info.Issuer == "" {
		t.Error("Issuer 不应为空")
	}
	if info.NotBefore == "" {
		t.Error("NotBefore 不应为空")
	}
	if info.NotAfter == "" {
		t.Error("NotAfter 不应为空")
	}
	if len(info.DNSNames) != 2 {
		t.Errorf("DNSNames 应有 2 个，实际 %d", len(info.DNSNames))
	}
	if info.CertSHA256 == "" {
		t.Error("CertSHA256 不应为空")
	}
	if info.KeySHA256 == "" {
		t.Error("KeySHA256 不应为空")
	}
}

func TestInspectPair_Expired(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t,
		time.Now().Add(-365*24*time.Hour),
		time.Now().Add(-24*time.Hour),
	)

	_, err := InspectPair(certPEM, keyPEM)
	if err == nil {
		t.Fatal("过期证书应返回错误")
	}
}

func TestInspectPair_MismatchedKey(t *testing.T) {
	// 生成两对不同证书/私钥
	certPEM, _ := generateTestCert(t,
		time.Now().Add(-24*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)
	_, wrongKeyPEM := generateTestCert(t,
		time.Now().Add(-24*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)

	_, err := InspectPair(certPEM, wrongKeyPEM)
	if err == nil {
		t.Fatal("不匹配的私钥应返回错误")
	}
}

func TestInspectPair_InvalidPEM(t *testing.T) {
	_, err := InspectPair([]byte("not a cert"), []byte("not a key"))
	if err == nil {
		t.Fatal("无效 PEM 应返回错误")
	}
}

func TestInspectCert_Valid(t *testing.T) {
	certPEM, _ := generateTestCert(t,
		time.Now().Add(-24*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)

	info, err := InspectCert(certPEM)
	if err != nil {
		t.Fatalf("InspectCert 失败: %v", err)
	}
	if len(info.DNSNames) != 2 {
		t.Errorf("DNSNames 应有 2 个，实际 %d", len(info.DNSNames))
	}
}

func TestInspectCert_Expired(t *testing.T) {
	certPEM, _ := generateTestCert(t,
		time.Now().Add(-365*24*time.Hour),
		time.Now().Add(-24*time.Hour),
	)

	_, err := InspectCert(certPEM)
	if err == nil {
		t.Fatal("过期证书应返回错误")
	}
}

func TestFirstPEMBlockType(t *testing.T) {
	certPEM, keyPEM := generateTestCert(t,
		time.Now().Add(-24*time.Hour),
		time.Now().Add(365*24*time.Hour),
	)

	if got := FirstPEMBlockType(certPEM); got != "CERTIFICATE" {
		t.Errorf("证书 PEM block type = %q, want CERTIFICATE", got)
	}
	if got := FirstPEMBlockType(keyPEM); got != "EC PRIVATE KEY" {
		t.Errorf("私钥 PEM block type = %q, want EC PRIVATE KEY", got)
	}
	if got := FirstPEMBlockType([]byte("garbage")); got != "" {
		t.Errorf("无效 PEM 应返回空字符串, got %q", got)
	}
}
