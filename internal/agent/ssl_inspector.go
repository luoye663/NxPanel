// agent 包 — SSL 证书检查 handler
//
// 处理 agent 内部 RPC：
//   - POST /internal/v1/ssl/inspect  解析证书信息
//
// 支持两种模式：
//   - PEM 内容模式：接收 cert_pem + key_pem
//   - 文件路径模式：接收 cert_path + key_path，agent 读取文件后解析
package agent

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/luoye663/nxpanel/internal/ssl"
)

// sslInspectRequest SSL 检查请求（两种模式共用）
type sslInspectRequest struct {
	// PEM 内容模式
	CertPEM string `json:"cert_pem"`
	KeyPEM  string `json:"key_pem"`

	// 文件路径模式
	CertPath string `json:"cert_path"`
	KeyPath  string `json:"key_path"`
}

// sslInspectResponse SSL 检查响应
type sslInspectResponse struct {
	Subject    string   `json:"subject"`
	Issuer     string   `json:"issuer"`
	NotBefore  string   `json:"not_before"`
	NotAfter   string   `json:"not_after"`
	DNSNames   []string `json:"dns_names"`
	CertSHA256 string   `json:"cert_sha256"`
	KeySHA256  string   `json:"key_sha256,omitempty"`
}

// handleSSLInspect 处理 SSL 证书检查请求
func (s *Server) handleSSLInspect(w http.ResponseWriter, r *http.Request) {
	var req sslInspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAgentError(w, http.StatusBadRequest, "请求体格式错误: "+err.Error())
		return
	}

	var info *ssl.CertInfo
	var err error

	if req.CertPEM != "" && req.KeyPEM != "" {
		// PEM 内容模式：校验证书和私钥匹配
		info, err = ssl.InspectPair([]byte(req.CertPEM), []byte(req.KeyPEM))
		if err != nil {
			writeAgentError(w, http.StatusUnprocessableEntity, "证书校验失败: "+err.Error())
			return
		}
		// 注意：不记录私钥内容
		slog.Info("SSL 证书检查通过 (PEM 模式)",
			"subject", info.Subject,
			"dns_names", strings.Join(info.DNSNames, ","),
		)
	} else if req.CertPath != "" && req.KeyPath != "" {
		// 文件路径模式：校验路径白名单后读取文件
		if _, err := s.policy.Validate(req.CertPath); err != nil {
			writeAgentError(w, http.StatusForbidden, "证书路径不在白名单内: "+err.Error())
			return
		}
		if _, err := s.policy.Validate(req.KeyPath); err != nil {
			writeAgentError(w, http.StatusForbidden, "私钥路径不在白名单内: "+err.Error())
			return
		}
		info, err = s.inspectSSLFiles(req.CertPath, req.KeyPath)
		if err != nil {
			writeAgentError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		slog.Info("SSL 证书检查通过 (文件模式)",
			"cert_path", req.CertPath,
			"subject", info.Subject,
		)
	} else {
		writeAgentError(w, http.StatusBadRequest, "必须提供 cert_pem+key_pem 或 cert_path+key_path")
		return
	}

	writeAgentOK(w, sslInspectResponse{
		Subject:    info.Subject,
		Issuer:     info.Issuer,
		NotBefore:  info.NotBefore,
		NotAfter:   info.NotAfter,
		DNSNames:   info.DNSNames,
		CertSHA256: info.CertSHA256,
		KeySHA256:  info.KeySHA256,
	})
}

// inspectSSLFiles 读取证书文件并校验
func (s *Server) inspectSSLFiles(certPath, keyPath string) (*ssl.CertInfo, error) {
	// 检查文件存在
	if _, err := os.Stat(certPath); err != nil {
		return nil, fmt.Errorf("证书文件不存在或不可读: %s", certPath)
	}
	if _, err := os.Stat(keyPath); err != nil {
		return nil, fmt.Errorf("私钥文件不存在或不可读: %s", keyPath)
	}

	// 读取文件
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("读取证书文件失败: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("读取私钥文件失败: %w", err)
	}

	// 校验证书和私钥匹配
	return ssl.InspectPair(certPEM, keyPEM)
}
