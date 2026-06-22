// ssl 包 — SSL 业务服务
//
// Service 封装 SSL 证书配置的全部业务逻辑：
//   - 获取 SSL 状态
//   - 上传 PEM 证书（manual_pem）
//   - 使用已有证书路径（existing_files）
//   - 禁用 SSL
//
// 安全要求：
//   - 私钥不写日志
//   - 私钥不写 operation message
//   - agent 写入私钥文件权限 0600
//   - API 返回永不包含私钥内容
package ssl

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
)

// SSLAgentClient 定义 SSL 服务所需的 agent RPC 能力
// 使用接口避免 ssl -> agentclient 循环依赖
type SSLAgentClient interface {
	SSLInspect(ctx context.Context, certPEM, keyPEM string) (*SSLInspectResult, error)
	SSLInspectFiles(ctx context.Context, certPath, keyPath string) (*SSLInspectResult, error)
	ApplyTransaction(ctx context.Context, req *TransactionRequest) error
	ReadFile(ctx context.Context, path string) ([]byte, error)
}

// SSLInspectResult 证书检查结果
type SSLInspectResult struct {
	Subject    string   `json:"subject"`
	Issuer     string   `json:"issuer"`
	NotBefore  string   `json:"not_before"`
	NotAfter   string   `json:"not_after"`
	DNSNames   []string `json:"dns_names"`
	CertSHA256 string   `json:"cert_sha256"`
	KeySHA256  string   `json:"key_sha256,omitempty"`
}

// TransactionRequest 文件事务请求（简化版，避免导入 agentclient）
type TransactionRequest struct {
	OperationID string
	Changes     []FileChange
	TestNginx   bool
	ReloadNginx bool
}

// FileChange 文件变更（简化版）
type FileChange struct {
	Type          string
	Path          string
	Target        string
	ContentBase64 string
	Perm          uint32
}

// Service SSL 业务服务
type Service struct {
	siteRepo *repo.SiteRepo
	sslRepo  *repo.SSLRepo
	certRepo *repo.CertificateRepo
	opRepo   *repo.OperationRepo
	agent    SSLAgentClient
	cfg      *app.Config
}

// NewService 创建 SSL 服务
func NewService(
	siteRepo *repo.SiteRepo,
	sslRepo *repo.SSLRepo,
	certRepo *repo.CertificateRepo,
	opRepo *repo.OperationRepo,
	agent SSLAgentClient,
	cfg *app.Config,
) *Service {
	return &Service{
		siteRepo: siteRepo,
		sslRepo:  sslRepo,
		certRepo: certRepo,
		opRepo:   opRepo,
		agent:    agent,
		cfg:      cfg,
	}
}

// SSLResponse GET SSL 状态的响应
// 对应文档 7.5.1 节
type SSLResponse struct {
	Enabled     bool     `json:"enabled"`
	Mode        string   `json:"mode"`
	CertPath    string   `json:"cert_path,omitempty"`
	KeyPath     string   `json:"key_path,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	Subject     string   `json:"subject,omitempty"`
	NotBefore   string   `json:"not_before,omitempty"`
	NotAfter    string   `json:"not_after,omitempty"`
	DNSNames    []string `json:"dns_names,omitempty"`
	ForceHTTPS  bool     `json:"force_https"`
	HSTSEnabled bool     `json:"hsts_enabled"`
}

// ManualPEMRequest 上传 PEM 证书请求
// 对应文档 7.5.2 节
type ManualPEMRequest struct {
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
	ForceHTTPS     bool   `json:"force_https"`
	HSTSEnabled    bool   `json:"hsts_enabled"`
}

// ExistingFilesRequest 使用已有证书路径请求
// 对应文档 7.5.3 节
type ExistingFilesRequest struct {
	CertPath    string `json:"cert_path"`
	KeyPath     string `json:"key_path"`
	ForceHTTPS  bool   `json:"force_https"`
	HSTSEnabled bool   `json:"hsts_enabled"`
}

// DisableSSLRequest 禁用 SSL 请求
// 对应文档 7.5.4 节
type DisableSSLRequest struct {
	DeleteManagedSSLFiles bool `json:"delete_managed_ssl_files"`
}

// Get 获取站点的 SSL 状态
func (svc *Service) Get(siteID string) (*SSLResponse, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	ssl, err := svc.sslRepo.GetBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	if ssl == nil || !ssl.Enabled {
		return &SSLResponse{
			Enabled:     false,
			Mode:        "disabled",
			ForceHTTPS:  true,
			HSTSEnabled: false,
		}, nil
	}

	var dnsNames []string
	if ssl.DNSNamesJSON != "" {
		json.Unmarshal([]byte(ssl.DNSNamesJSON), &dnsNames)
	}

	resp := &SSLResponse{
		Enabled:     true,
		Mode:        ssl.Mode,
		CertPath:    ssl.CertPath,
		KeyPath:     ssl.KeyPath,
		Issuer:      ssl.Issuer,
		Subject:     ssl.Subject,
		NotBefore:   ptrToString(ssl.NotBefore),
		NotAfter:    ptrToString(ssl.NotAfter),
		DNSNames:    dnsNames,
		ForceHTTPS:  ssl.ForceHTTPS,
		HSTSEnabled: ssl.HSTSEnabled,
	}

	return resp, nil
}

// ManualPEM 上传 PEM 证书并启用 SSL
//
// 流程：
//  1. 基本校验（非空、大小限制 512KB）
//  2. 调用 agent 的 SSL inspect RPC 解析证书
//  3. 更新 DB
//  4. 重新渲染 Nginx 配置
//  5. 通过 agent 写入文件（cert 0644, key 0600）
func (svc *Service) ManualPEM(ctx context.Context, siteID string, req *ManualPEMRequest, requestID string) (*SSLResponse, string, error) {
	// 1. 基本校验
	if req.CertificatePEM == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书内容不能为空", nil)
	}
	if req.PrivateKeyPEM == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥内容不能为空", nil)
	}
	// 大小限制 512KB
	if len(req.CertificatePEM) > 512*1024 {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书内容超过 512KB 限制", nil)
	}
	if len(req.PrivateKeyPEM) > 512*1024 {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥内容超过 512KB 限制", nil)
	}

	// 检查 PEM 格式
	if FirstPEMBlockType([]byte(req.CertificatePEM)) != "CERTIFICATE" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书 PEM 格式不正确，第一个 block 应为 CERTIFICATE", nil)
	}
	keyType := FirstPEMBlockType([]byte(req.PrivateKeyPEM))
	if keyType == "" || !strings.HasSuffix(keyType, "PRIVATE KEY") {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥 PEM 格式不正确", nil)
	}

	// 2. 检查站点
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	// 3. 调用 agent 的 SSL inspect RPC 解析证书
	inspectResp, err := svc.agent.SSLInspect(ctx, req.CertificatePEM, req.PrivateKeyPEM)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书校验失败: "+err.Error(), nil)
	}

	// 4. 生成托管路径
	panelDir := svc.cfg.Nginx.PanelDir
	certPath := filepath.Join(panelDir, "ssl", siteID, "fullchain.pem")
	keyPath := filepath.Join(panelDir, "ssl", siteID, "privkey.pem")

	previousSSL, _ := svc.sslRepo.GetBySiteID(siteID)

	dnsNamesJSON, _ := json.Marshal(inspectResp.DNSNames)
	sslRow := &repo.SiteSSL{
		SiteID:       siteID,
		Enabled:      true,
		Mode:         "manual_pem",
		CertPath:     certPath,
		KeyPath:      keyPath,
		CertSHA256:   inspectResp.CertSHA256,
		KeySHA256:    inspectResp.KeySHA256,
		Issuer:       inspectResp.Issuer,
		Subject:      inspectResp.Subject,
		NotBefore:    &inspectResp.NotBefore,
		NotAfter:     &inspectResp.NotAfter,
		DNSNamesJSON: string(dnsNamesJSON),
		ForceHTTPS:   req.ForceHTTPS,
		HSTSEnabled:  req.HSTSEnabled,
	}
	if err := svc.sslRepo.Upsert(sslRow); err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "保存 SSL 配置失败: "+err.Error(), nil)
	}

	configContent, err := svc.renderConfig(ctx, site, sslRow)
	if err != nil {
		return nil, "", err
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.update_ssl", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("启用 SSL (manual_pem) 站点 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	// 注意：私钥不写入日志
	changes := []FileChange{
		{
			Type:          "write",
			Path:          certPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(req.CertificatePEM)),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          keyPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(req.PrivateKeyPEM)),
			Perm:          0600, // 私钥文件权限 0600
		},
		{
			Type:          "write",
			Path:          site.ConfigPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(configContent)),
			Perm:          0644,
		},
	}

	agentErr := svc.agent.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		svc.rollbackSSL(siteID, previousSSL)
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "文件事务失败: "+agentErr.Error(), nil)
	}

	_ = svc.siteRepo.Update(site)
	_ = svc.opRepo.UpdateStatus(opID, "success")

	slog.Info("SSL manual_pem 启用成功", "site_id", siteID, "operation_id", opID)

	svc.syncToStore(inspectResp, certPath, keyPath, site.PrimaryDomain)

	return &SSLResponse{
		Enabled:    true,
		Mode:       "manual_pem",
		CertPath:   certPath,
		NotAfter:   inspectResp.NotAfter,
		DNSNames:   inspectResp.DNSNames,
		ForceHTTPS: req.ForceHTTPS,
	}, opID, nil
}

// ExistingFiles 使用已有证书路径启用 SSL
func (svc *Service) ExistingFiles(ctx context.Context, siteID string, req *ExistingFilesRequest, requestID string) (*SSLResponse, string, error) {
	// 校验路径
	if req.CertPath == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书路径不能为空", nil)
	}
	if req.KeyPath == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥路径不能为空", nil)
	}
	if !filepath.IsAbs(req.CertPath) {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书路径必须是绝对路径", nil)
	}
	if !filepath.IsAbs(req.KeyPath) {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥路径必须是绝对路径", nil)
	}
	// 禁止空字节和换行
	if strings.ContainsAny(req.CertPath, "\x00\n\r") || strings.ContainsAny(req.KeyPath, "\x00\n\r") {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "路径不允许包含空字节或换行", nil)
	}

	// 检查站点
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	// 调用 agent 检查文件存在并解析证书
	inspectResp, err := svc.agent.SSLInspectFiles(ctx, req.CertPath, req.KeyPath)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书文件检查失败: "+err.Error(), nil)
	}

	previousSSL, _ := svc.sslRepo.GetBySiteID(siteID)

	dnsNamesJSON, _ := json.Marshal(inspectResp.DNSNames)
	sslRow := &repo.SiteSSL{
		SiteID:       siteID,
		Enabled:      true,
		Mode:         "existing_files",
		CertPath:     req.CertPath,
		KeyPath:      req.KeyPath,
		CertSHA256:   inspectResp.CertSHA256,
		Issuer:       inspectResp.Issuer,
		Subject:      inspectResp.Subject,
		NotBefore:    &inspectResp.NotBefore,
		NotAfter:     &inspectResp.NotAfter,
		DNSNamesJSON: string(dnsNamesJSON),
		ForceHTTPS:   req.ForceHTTPS,
		HSTSEnabled:  req.HSTSEnabled,
	}
	if err := svc.sslRepo.Upsert(sslRow); err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "保存 SSL 配置失败: "+err.Error(), nil)
	}

	configContent, err := svc.renderConfig(ctx, site, sslRow)
	if err != nil {
		return nil, "", err
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.update_ssl", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("启用 SSL (existing_files) 站点 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	agentErr := svc.agent.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: opID,
		Changes: []FileChange{
			{
				Type:          "write",
				Path:          site.ConfigPath,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(configContent)),
				Perm:          0644,
			},
		},
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		svc.rollbackSSL(siteID, previousSSL)
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, agentErr.Error(), nil)
	}

	_ = svc.siteRepo.Update(site)
	_ = svc.opRepo.UpdateStatus(opID, "success")

	svc.syncToStore(inspectResp, req.CertPath, req.KeyPath, site.PrimaryDomain)

	return &SSLResponse{
		Enabled:    true,
		Mode:       "existing_files",
		CertPath:   req.CertPath,
		NotAfter:   inspectResp.NotAfter,
		ForceHTTPS: req.ForceHTTPS,
	}, opID, nil
}

// Disable 禁用 SSL
func (svc *Service) Disable(ctx context.Context, siteID string, req *DisableSSLRequest, requestID string) (*SSLResponse, string, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	previousSSL, _ := svc.sslRepo.GetBySiteID(siteID)

	disabledRow := &repo.SiteSSL{
		SiteID:      siteID,
		Enabled:     false,
		Mode:        "disabled",
		ForceHTTPS:  true,
		HSTSEnabled: false,
	}
	if err := svc.sslRepo.Upsert(disabledRow); err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "更新 SSL 配置失败: "+err.Error(), nil)
	}

	configContent, err := svc.renderConfig(ctx, site, disabledRow)
	if err != nil {
		return nil, "", err
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.update_ssl", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("禁用 SSL 站点 %s", site.PrimaryDomain),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	changes := []FileChange{
		{
			Type:          "write",
			Path:          site.ConfigPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(configContent)),
			Perm:          0644,
		},
	}

	if req.DeleteManagedSSLFiles && previousSSL != nil && previousSSL.Mode == "manual_pem" {
		inStore, _ := svc.certRepo.GetBySHA256(previousSSL.CertSHA256)
		if inStore == nil {
			changes = append(changes,
				FileChange{Type: "remove", Path: previousSSL.CertPath},
				FileChange{Type: "remove", Path: previousSSL.KeyPath},
			)
		}
	}

	agentErr := svc.agent.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		svc.rollbackSSL(siteID, previousSSL)
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, agentErr.Error(), nil)
	}

	_ = svc.siteRepo.Update(site)
	_ = svc.opRepo.UpdateStatus(opID, "success")

	return &SSLResponse{
		Enabled: false,
		Mode:    "disabled",
	}, opID, nil
}

// syncToStore 将已使用的证书记录同步到证书夹
// 如果相同 SHA256 的证书已存在于证书夹，则跳过
func (svc *Service) rollbackSSL(siteID string, previous *repo.SiteSSL) {
	if previous != nil {
		if err := svc.sslRepo.Upsert(previous); err != nil {
			slog.Error("回滚 SSL 配置失败", "site_id", siteID, "error", err)
		}
	} else {
		if err := svc.sslRepo.Delete(siteID); err != nil {
			slog.Error("回滚 SSL 配置失败（删除）", "site_id", siteID, "error", err)
		}
	}
}

func (svc *Service) syncToStore(inspectResp *SSLInspectResult, certPath, keyPath, primaryDomain string) {
	existing, _ := svc.certRepo.GetBySHA256(inspectResp.CertSHA256)
	if existing != nil {
		return
	}

	certID := app.NewID("cert")
	dnsNamesJSON, _ := json.Marshal(inspectResp.DNSNames)
	cert := &repo.Certificate{
		ID:          certID,
		Name:        primaryDomain,
		DomainsJSON: string(dnsNamesJSON),
		Issuer:      inspectResp.Issuer,
		Subject:     inspectResp.Subject,
		NotBefore:   &inspectResp.NotBefore,
		NotAfter:    &inspectResp.NotAfter,
		CertSHA256:  inspectResp.CertSHA256,
		KeySHA256:   inspectResp.KeySHA256,
		CertPath:    certPath,
		KeyPath:     keyPath,
	}
	if err := svc.certRepo.Create(cert); err != nil {
		slog.Warn("同步证书记录到证书夹失败", "cert_path", certPath, "error", err)
	} else {
		slog.Info("证书记录已同步到证书夹", "cert_id", certID, "name", primaryDomain)
	}
}

// renderConfig 通过 marker 增量补丁更新站点 Nginx 配置中的 SSL 相关部分
// 仅修改 LISTEN、SSL、FORCE-HTTPS 三个 marker 块，保留其他所有内容
func (svc *Service) renderConfig(ctx context.Context, site *repo.Site, sslRow *repo.SiteSSL) (string, error) {
	currentContent, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return "", app.NewAppError(app.ErrAgentUnavailable, "读取配置文件失败: "+err.Error(), nil)
	}

	requiredMarkers := []string{
		nginx.MarkerNameListen,
	}
	markerStatus := nginx.ValidateRequiredMarkers(currentContent, requiredMarkers)
	if !markerStatus.Valid {
		return "", app.NewAppError(app.ErrValidationFailed,
			fmt.Sprintf("配置文件缺少必要标识块: missing=%v duplicated=%v", markerStatus.Missing, markerStatus.Duplicated), nil)
	}

	var bindings []repo.Binding
	if site.BindingsJSON != "" {
		json.Unmarshal([]byte(site.BindingsJSON), &bindings)
	}
	if len(bindings) == 0 {
		var domains []string
		json.Unmarshal([]byte(site.DomainsJSON), &domains)
		for _, d := range domains {
			bindings = append(bindings, repo.Binding{Domain: d, Port: site.HTTPPort})
		}
	}

	defaultServer := detectDefaultServer(currentContent)

	renderData := &nginx.RenderData{
		HTTPPort:      site.HTTPPort,
		HTTPSPort:     site.HTTPSPort,
		DefaultServer: defaultServer,
		Bindings:      bindings,
	}

	if sslRow != nil && sslRow.Enabled {
		renderData.SSL = &nginx.SSLData{
			Enabled:    true,
			Mode:       sslRow.Mode,
			CertPath:   sslRow.CertPath,
			KeyPath:    sslRow.KeyPath,
			ForceHTTPS: sslRow.ForceHTTPS,
		}
	}

	listenBlock := nginx.BuildListenBlock(renderData)
	sslBlock := nginx.BuildSSLBlock(renderData)
	forceHTTPSBlock := nginx.BuildForceHTTPSBlock(renderData)

	patched, err := nginx.ApplyMarkerPatches(currentContent, []nginx.BlockPatch{
		{Name: nginx.MarkerNameListen, Body: []byte(listenBlock)},
	})
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, "标识块替换失败: "+err.Error(), nil)
	}

	patched, err = nginx.ApplyOptionalMarkerPatches(patched, []nginx.BlockPatch{
		{Name: nginx.MarkerNameSSL, Body: []byte(sslBlock)},
		{Name: nginx.MarkerNameForceHTTPS, Body: []byte(forceHTTPSBlock)},
	})
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, "标识块替换失败: "+err.Error(), nil)
	}

	return string(patched), nil
}

// detectDefaultServer 从当前配置的 LISTEN marker 块中检测是否包含 default_server
func detectDefaultServer(content []byte) bool {
	block, err := nginx.ExtractMarkerBlock(content, nginx.MarkerNameListen)
	if err != nil {
		return false
	}
	return bytes.Contains(block, []byte("default_server"))
}

// CertificateResponse 证书夹条目响应
type CertificateResponse struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Domains    []string `json:"domains"`
	Issuer     string   `json:"issuer"`
	Subject    string   `json:"subject"`
	NotBefore  string   `json:"not_before,omitempty"`
	NotAfter   string   `json:"not_after,omitempty"`
	CertSHA256 string   `json:"cert_sha256"`
	CertPath   string   `json:"cert_path"`
	KeyPath    string   `json:"key_path"`
	CreatedAt  string   `json:"created_at"`
}

// UploadToStoreRequest 上传证书到证书夹请求
type UploadToStoreRequest struct {
	Name           string `json:"name"`
	CertificatePEM string `json:"certificate_pem"`
	PrivateKeyPEM  string `json:"private_key_pem"`
}

// DeployFromStoreRequest 从证书夹部署到站点请求
type DeployFromStoreRequest struct {
	SiteID      string `json:"site_id"`
	ForceHTTPS  bool   `json:"force_https"`
	HSTSEnabled bool   `json:"hsts_enabled"`
}

// ListStoreCertificates 获取证书夹中所有证书
func (svc *Service) ListStoreCertificates() ([]*CertificateResponse, error) {
	certs, err := svc.certRepo.List()
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}

	result := make([]*CertificateResponse, 0, len(certs))
	for _, c := range certs {
		var domains []string
		if c.DomainsJSON != "" {
			json.Unmarshal([]byte(c.DomainsJSON), &domains)
		}
		result = append(result, &CertificateResponse{
			ID:         c.ID,
			Name:       c.Name,
			Domains:    domains,
			Issuer:     c.Issuer,
			Subject:    c.Subject,
			NotBefore:  ptrToString(c.NotBefore),
			NotAfter:   ptrToString(c.NotAfter),
			CertSHA256: c.CertSHA256,
			CertPath:   c.CertPath,
			KeyPath:    c.KeyPath,
			CreatedAt:  c.CreatedAt,
		})
	}
	return result, nil
}

// UploadToStore 上传证书到证书夹
func (svc *Service) UploadToStore(ctx context.Context, req *UploadToStoreRequest, requestID string) (*CertificateResponse, string, error) {
	if req.Name == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书名称不能为空", nil)
	}
	if req.CertificatePEM == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书内容不能为空", nil)
	}
	if req.PrivateKeyPEM == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥内容不能为空", nil)
	}
	if len(req.CertificatePEM) > 512*1024 {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书内容超过 512KB 限制", nil)
	}
	if len(req.PrivateKeyPEM) > 512*1024 {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥内容超过 512KB 限制", nil)
	}

	if FirstPEMBlockType([]byte(req.CertificatePEM)) != "CERTIFICATE" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书 PEM 格式不正确", nil)
	}
	keyType := FirstPEMBlockType([]byte(req.PrivateKeyPEM))
	if keyType == "" || !strings.HasSuffix(keyType, "PRIVATE KEY") {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "私钥 PEM 格式不正确", nil)
	}

	inspectResp, err := svc.agent.SSLInspect(ctx, req.CertificatePEM, req.PrivateKeyPEM)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书校验失败: "+err.Error(), nil)
	}

	existing, _ := svc.certRepo.GetBySHA256(inspectResp.CertSHA256)
	if existing != nil {
		return nil, "", app.NewAppError(app.ErrConflict, "相同指纹的证书已存在于证书夹中: "+existing.Name, nil)
	}

	certID := app.NewID("cert")
	panelDir := svc.cfg.Nginx.PanelDir
	certPath := filepath.Join(panelDir, "ssl-store", certID, "fullchain.pem")
	keyPath := filepath.Join(panelDir, "ssl-store", certID, "privkey.pem")

	dnsNamesJSON, _ := json.Marshal(inspectResp.DNSNames)
	cert := &repo.Certificate{
		ID:          certID,
		Name:        req.Name,
		DomainsJSON: string(dnsNamesJSON),
		Issuer:      inspectResp.Issuer,
		Subject:     inspectResp.Subject,
		NotBefore:   &inspectResp.NotBefore,
		NotAfter:    &inspectResp.NotAfter,
		CertSHA256:  inspectResp.CertSHA256,
		KeySHA256:   inspectResp.KeySHA256,
		CertPath:    certPath,
		KeyPath:     keyPath,
	}
	if err := svc.certRepo.Create(cert); err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "保存证书记录失败: "+err.Error(), nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "cert.upload", TargetType: "certificate", TargetID: certID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("上传证书到证书夹: %s", req.Name),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	changes := []FileChange{
		{
			Type:          "write",
			Path:          certPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(req.CertificatePEM)),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          keyPath,
			ContentBase64: base64.StdEncoding.EncodeToString([]byte(req.PrivateKeyPEM)),
			Perm:          0600,
		},
	}

	agentErr := svc.agent.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   false,
		ReloadNginx: false,
	})
	if agentErr != nil {
		_ = svc.certRepo.Delete(certID)
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "写入证书文件失败: "+agentErr.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")
	slog.Info("证书已上传到证书夹", "cert_id", certID, "name", req.Name)

	var domains []string
	json.Unmarshal([]byte(string(dnsNamesJSON)), &domains)

	return &CertificateResponse{
		ID:         certID,
		Name:       req.Name,
		Domains:    domains,
		Issuer:     inspectResp.Issuer,
		Subject:    inspectResp.Subject,
		NotBefore:  inspectResp.NotBefore,
		NotAfter:   inspectResp.NotAfter,
		CertSHA256: inspectResp.CertSHA256,
		CertPath:   certPath,
		KeyPath:    keyPath,
	}, opID, nil
}

// DeleteFromStore 从证书夹删除证书
func (svc *Service) DeleteFromStore(ctx context.Context, certID string, requestID string) (string, error) {
	cert, err := svc.certRepo.GetByID(certID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if cert == nil {
		return "", app.NewAppError(app.ErrNotFound, "证书不存在", nil)
	}

	deployedCount, _ := svc.sslRepo.CountByCertStoreID(certID)
	if deployedCount > 0 {
		return "", app.NewAppError(app.ErrConflict, fmt.Sprintf("该证书已被 %d 个站点使用，请先禁用相关站点的 SSL", deployedCount), nil)
	}

	if err := svc.certRepo.Delete(certID); err != nil {
		return "", app.NewAppError(app.ErrInternalError, "删除证书记录失败: "+err.Error(), nil)
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "cert.delete", TargetType: "certificate", TargetID: certID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("从证书夹删除证书: %s", cert.Name),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	changes := []FileChange{
		{Type: "remove", Path: cert.CertPath},
		{Type: "remove", Path: cert.KeyPath},
	}

	agentErr := svc.agent.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   false,
		ReloadNginx: false,
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		return "", app.NewAppError(app.ErrAgentUnavailable, "删除证书文件失败: "+agentErr.Error(), nil)
	}

	_ = svc.opRepo.UpdateStatus(opID, "success")
	slog.Info("证书已从证书夹删除", "cert_id", certID, "name", cert.Name)

	return opID, nil
}

// DeployFromStore 从证书夹部署证书到站点
func (svc *Service) DeployFromStore(ctx context.Context, certID string, req *DeployFromStoreRequest, requestID string) (*SSLResponse, string, error) {
	cert, err := svc.certRepo.GetByID(certID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if cert == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "证书不存在", nil)
	}

	site, err := svc.siteRepo.GetByID(req.SiteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	inspectResp, err := svc.agent.SSLInspectFiles(ctx, cert.CertPath, cert.KeyPath)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "证书文件检查失败: "+err.Error(), nil)
	}

	previousSSL, _ := svc.sslRepo.GetBySiteID(req.SiteID)

	dnsNamesJSON, _ := json.Marshal(inspectResp.DNSNames)
	sslRow := &repo.SiteSSL{
		SiteID:       req.SiteID,
		Enabled:      true,
		Mode:         "from_store",
		CertPath:     cert.CertPath,
		KeyPath:      cert.KeyPath,
		CertSHA256:   inspectResp.CertSHA256,
		Issuer:       inspectResp.Issuer,
		Subject:      inspectResp.Subject,
		NotBefore:    &inspectResp.NotBefore,
		NotAfter:     &inspectResp.NotAfter,
		DNSNamesJSON: string(dnsNamesJSON),
		ForceHTTPS:   req.ForceHTTPS,
		HSTSEnabled:  req.HSTSEnabled,
		CertStoreID:  &certID,
	}
	if err := svc.sslRepo.Upsert(sslRow); err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "保存 SSL 配置失败: "+err.Error(), nil)
	}

	configContent, err := svc.renderConfig(ctx, site, sslRow)
	if err != nil {
		return nil, "", err
	}

	opID := app.NewOperationID()
	_ = svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "site.deploy_cert", TargetType: "site", TargetID: req.SiteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("从证书夹部署 SSL 到站点 %s (证书: %s)", site.PrimaryDomain, cert.Name),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	agentErr := svc.agent.ApplyTransaction(ctx, &TransactionRequest{
		OperationID: opID,
		Changes: []FileChange{
			{
				Type:          "write",
				Path:          site.ConfigPath,
				ContentBase64: base64.StdEncoding.EncodeToString([]byte(configContent)),
				Perm:          0644,
			},
		},
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, agentErr.Error(), "")
		svc.rollbackSSL(req.SiteID, previousSSL)
		return nil, "", app.NewAppError(app.ErrAgentUnavailable, "文件事务失败: "+agentErr.Error(), nil)
	}

	_ = svc.siteRepo.Update(site)
	_ = svc.opRepo.UpdateStatus(opID, "success")

	slog.Info("从证书夹部署 SSL 成功", "site_id", req.SiteID, "cert_id", certID, "operation_id", opID)

	return &SSLResponse{
		Enabled:    true,
		Mode:       "from_store",
		CertPath:   cert.CertPath,
		KeyPath:    cert.KeyPath,
		Issuer:     inspectResp.Issuer,
		Subject:    inspectResp.Subject,
		NotBefore:  inspectResp.NotBefore,
		NotAfter:   inspectResp.NotAfter,
		DNSNames:   inspectResp.DNSNames,
		ForceHTTPS: req.ForceHTTPS,
	}, opID, nil
}

// GetContent 读取当前 SSL 证书和私钥的 PEM 内容
func (svc *Service) GetContent(ctx context.Context, siteID string) (string, string, error) {
	ssl, err := svc.sslRepo.GetBySiteID(siteID)
	if err != nil {
		return "", "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if ssl == nil || !ssl.Enabled {
		return "", "", app.NewAppError(app.ErrValidationFailed, "SSL 未启用", nil)
	}

	certPEM, err := svc.agent.ReadFile(ctx, ssl.CertPath)
	if err != nil {
		return "", "", app.NewAppError(app.ErrInternalError, "读取证书文件失败: "+err.Error(), nil)
	}
	keyPEM, err := svc.agent.ReadFile(ctx, ssl.KeyPath)
	if err != nil {
		return "", "", app.NewAppError(app.ErrInternalError, "读取私钥文件失败: "+err.Error(), nil)
	}

	return string(certPEM), string(keyPEM), nil
}

// DownloadZIP 将当前证书打包为 ZIP 返回
func (svc *Service) DownloadZIP(ctx context.Context, siteID string) ([]byte, string, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	certPEM, keyPEM, err := svc.GetContent(ctx, siteID)
	if err != nil {
		return nil, "", err
	}

	zipBytes, err := createSSLZip([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "打包失败: "+err.Error(), nil)
	}

	filename := site.PrimaryDomain + "_ssl.zip"
	if filename == "_ssl.zip" {
		filename = "certificate.zip"
	}
	return zipBytes, filename, nil
}

// ptrToString 安全解引用字符串指针
func ptrToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func createSSLZip(certPEM, keyPEM []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	f, err := w.Create("fullchain.pem")
	if err != nil {
		return nil, err
	}
	f.Write(certPEM)

	f, err = w.Create("privkey.pem")
	if err != nil {
		return nil, err
	}
	f.Write(keyPEM)

	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
