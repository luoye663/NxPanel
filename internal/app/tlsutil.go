package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func GenerateSelfSignedCert(certPath, keyPath string, validity time.Duration, extraSANs []string) error {
	if err := os.MkdirAll(filepath.Dir(certPath), 0700); err != nil {
		return fmt.Errorf("create cert directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0700); err != nil {
		return fmt.Errorf("create key directory: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate ECDSA key: %w", err)
	}

	dnsNames := []string{"localhost"}
	var ipAddrs []net.IP
	for _, s := range []string{"127.0.0.1", "::1"} {
		if ip := net.ParseIP(s); ip != nil {
			ipAddrs = append(ipAddrs, ip)
		}
	}

	for _, s := range extraSANs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if ip := net.ParseIP(s); ip != nil {
			ipAddrs = append(ipAddrs, ip)
		} else {
			dnsNames = append(dnsNames, s)
		}
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("generate serial number: %w", err)
	}

	uuid, err := generateUUID()
	if err != nil {
		return fmt.Errorf("generate UUID: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{CommonName: uuid},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dnsNames,
		IPAddresses:  ipAddrs,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("write cert file: %w", err)
	}

	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal ECDSA private key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("write key file: %w", err)
	}

	return nil
}

func IsCertExpiredOrMissing(certPath string) bool {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return true
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return true
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return true
	}
	return time.Until(cert.NotAfter) < 24*time.Hour
}

func EnsureAPICertificate(tlsCfg *TLSConfig, dataDir string) (certPath, keyPath string, err error) {
	if tlsCfg.Cert != "" && tlsCfg.Key != "" {
		certPath = tlsCfg.Cert
		keyPath = tlsCfg.Key
	} else {
		tlsDir := filepath.Join(dataDir, "tls")
		certPath = filepath.Join(tlsDir, "api.crt")
		keyPath = filepath.Join(tlsDir, "api.key")
	}

	if !IsCertExpiredOrMissing(certPath) {
		if _, err := os.Stat(keyPath); err == nil {
			return certPath, keyPath, nil
		}
	}

	validity := ParseDurationOrDefault(tlsCfg.CertValidity, 8760*time.Hour)
	if err := GenerateSelfSignedCert(certPath, keyPath, validity, tlsCfg.SANs); err != nil {
		return "", "", fmt.Errorf("generate self-signed certificate: %w", err)
	}

	return certPath, keyPath, nil
}

func NewAPITLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
}

func generateUUID() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:]), nil
}
