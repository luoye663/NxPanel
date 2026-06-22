// ssl 包 — SSL 证书解析与校验
//
// 提供 PEM 证书/私钥的解析、校验和信息提取功能。
//
// 安全要求：
//   - 私钥不入 SQLite
//   - 私钥不写日志
//   - API 返回中永不包含私钥内容
package ssl

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// CertInfo 证书解析后的元信息
// 对应 site_ssl 表中的字段
type CertInfo struct {
	Subject    string   `json:"subject"`
	Issuer     string   `json:"issuer"`
	NotBefore  string   `json:"not_before"`
	NotAfter   string   `json:"not_after"`
	DNSNames   []string `json:"dns_names"`
	CertSHA256 string   `json:"cert_sha256"`
	KeySHA256  string   `json:"key_sha256"`
}

// InspectPair 解析并校验证书/私钥对
//
// 校验流程：
//  1. tls.X509KeyPair 校验证书和私钥匹配
//  2. 解析证书元信息
//  3. 检查证书未过期
//  4. 检查公钥类型受支持（RSA/ECDSA）
//
// 返回证书元信息，或错误。
func InspectPair(certPEM, keyPEM []byte) (*CertInfo, error) {
	// 校验证书和私钥匹配
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("证书和私钥不匹配或格式错误: %w", err)
	}
	if len(pair.Certificate) == 0 {
		return nil, errors.New("证书链为空")
	}

	cert, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("解析证书失败: %w", err)
	}

	// 检查证书是否过期
	if time.Now().After(cert.NotAfter) {
		return nil, errors.New("证书已过期")
	}

	// 检查公钥类型
	if !publicKeySupported(cert.PublicKey) {
		return nil, errors.New("不支持的公钥类型")
	}

	// 计算 SHA256 指纹
	certHash := sha256SumPEM(certPEM)
	keyHash := sha256SumPEM(keyPEM)

	return &CertInfo{
		Subject:    cert.Subject.String(),
		Issuer:     cert.Issuer.String(),
		NotBefore:  cert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:   cert.NotAfter.UTC().Format(time.RFC3339),
		DNSNames:   cert.DNSNames,
		CertSHA256: certHash,
		KeySHA256:  keyHash,
	}, nil
}

// InspectCert 只解析证书（不校验私钥匹配）
// 用于 existing_files 模式下只解析证书信息
func InspectCert(certPEM []byte) (*CertInfo, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("无法解码 PEM 证书")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析证书失败: %w", err)
	}

	if time.Now().After(cert.NotAfter) {
		return nil, errors.New("证书已过期")
	}

	if !publicKeySupported(cert.PublicKey) {
		return nil, errors.New("不支持的公钥类型")
	}

	return &CertInfo{
		Subject:    cert.Subject.String(),
		Issuer:     cert.Issuer.String(),
		NotBefore:  cert.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:   cert.NotAfter.UTC().Format(time.RFC3339),
		DNSNames:   cert.DNSNames,
		CertSHA256: sha256SumPEM(certPEM),
	}, nil
}

// FirstPEMBlockType 返回 PEM 数据中第一个 block 的类型
// 用于判断上传的是证书还是私钥
func FirstPEMBlockType(data []byte) string {
	block, _ := pem.Decode(data)
	if block == nil {
		return ""
	}
	return block.Type
}

// publicKeySupported 检查公钥类型是否受支持
func publicKeySupported(k any) bool {
	switch k.(type) {
	case *rsa.PublicKey, *ecdsa.PublicKey:
		return true
	default:
		return false
	}
}

// sha256SumPEM 计算 PEM 数据的 SHA256 哈希
func sha256SumPEM(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
