package ssl

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

type ReplacementRequest struct {
	DependencyHash    string `json:"dependency_hash"`
	CertificateID     string `json:"certificate_id"`
	ExpectedHash      string `json:"expected_hash"`
	CertificateSHA256 string `json:"certificate_sha256"`
}
type ReplacementPreview struct {
	DependencyHash    string `json:"dependency_hash"`
	checks            []FileChange
	FileHash          string   `json:"file_hash"`
	CertificateSHA256 string   `json:"certificate_sha256"`
	OldCertPath       string   `json:"old_cert_path"`
	NewCertPath       string   `json:"new_cert_path"`
	OldKeyPath        string   `json:"old_key_path"`
	NewKeyPath        string   `json:"new_key_path"`
	NotAfter          string   `json:"not_after"`
	MatchedDomains    []string `json:"matched_domains"`
	UnmatchedDomains  []string `json:"unmatched_domains"`
}

func certificateDomainCoverage(domains, names []string) ([]string, []string) {
	matched, unmatched := []string{}, []string{}
	seen := make(map[string]bool)
	normalize := func(s string) string { return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".") }
	for _, domain := range domains {
		domain = normalize(domain)
		if domain == "" || seen[domain] {
			continue
		}
		seen[domain] = true
		covered := false
		for _, name := range names {
			name = normalize(name)
			if name == domain || (strings.HasPrefix(name, "*.") && strings.HasSuffix(domain, name[1:]) && strings.Count(domain, ".") == strings.Count(name, ".")) {
				covered = true
				break
			}
		}
		if covered {
			matched = append(matched, domain)
		} else {
			unmatched = append(unmatched, domain)
		}
	}
	return matched, unmatched
}

func (svc *Service) replacement(ctx context.Context, id string, req *ReplacementRequest) (*ReplacementPreview, string, *repo.Certificate, *SSLInspectResult, error) {
	site, err := svc.siteRepo.GetByID(id)
	if err != nil {
		return nil, "", nil, nil, err
	}
	if site == nil {
		return nil, "", nil, nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	content, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return nil, "", nil, nil, app.NewAppError(app.ErrAgentUnavailable, "读取配置失败", nil)
	}
	ref, checks, resolveErr := svc.resolveCertificate(ctx, string(content), site.PrimaryDomain)
	if resolveErr != nil {
		return nil, "", nil, nil, app.NewAppError(app.ErrValidationFailed, "只能只读展示："+resolveErr.Error(), nil)
	}
	cert, err := svc.certRepo.GetByID(req.CertificateID)
	if err != nil {
		return nil, "", nil, nil, err
	}
	if cert == nil {
		return nil, "", nil, nil, app.NewAppError(app.ErrNotFound, "证书不存在", nil)
	}
	// Uploaded store files are immutable and already live in panel-owned
	// directories. Never redirect a site to arbitrary external key paths.
	root := filepath.Join(svc.cfg.Nginx.PanelDir, "ssl-store") + string(filepath.Separator)
	if cert.Source == "external" || !strings.HasPrefix(cert.CertPath, root) || !strings.HasPrefix(cert.KeyPath, root) {
		return nil, "", nil, nil, app.NewAppError(app.ErrValidationFailed, "请先上传新证书到证书夹；局部替换仅使用面板 ssl-store 中的文件", nil)
	}
	info, err := svc.agent.SSLInspectFiles(ctx, cert.CertPath, cert.KeyPath)
	if err != nil {
		return nil, "", nil, nil, app.NewAppError(app.ErrValidationFailed, "新证书无法校验，请检查证书夹文件与 Agent 白名单", nil)
	}
	for _, hash := range []string{info.CertSHA256, info.KeySHA256} {
		decoded, err := hex.DecodeString(hash)
		if err != nil || len(decoded) != 32 {
			return nil, "", nil, nil, app.NewAppError(app.ErrAgentUnavailable, "Agent 未返回有效的证书或私钥校验信息，未修改配置；请检查 API 与 Agent 版本并重新预览", nil)
		}
	}
	if info.CertSHA256 != cert.CertSHA256 {
		return nil, "", nil, nil, app.NewAppError(app.ErrConflict, "证书夹文件发生变化，请重新上传", nil)
	}
	var domains []string
	_ = json.Unmarshal([]byte(site.DomainsJSON), &domains)
	domains = append(domains, site.PrimaryDomain)
	matched, unmatched := certificateDomainCoverage(domains, info.DNSNames)
	if len(matched) == 0 {
		return nil, "", nil, nil, app.NewAppError(app.ErrValidationFailed, "新证书未覆盖任何已绑定域名", map[string]any{"unmatched_domains": unmatched})
	}
	patched, err := nginxpatch.ReplaceResolvedCertificate(string(content), site.PrimaryDomain, ref.FileHash, cert.CertPath, cert.KeyPath)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return &ReplacementPreview{checks: checks, DependencyHash: dependencyHash(checks), FileHash: ref.FileHash, CertificateSHA256: info.CertSHA256, OldCertPath: ref.CertPath, OldKeyPath: ref.KeyPath, NewCertPath: cert.CertPath, NewKeyPath: cert.KeyPath, NotAfter: info.NotAfter, MatchedDomains: matched, UnmatchedDomains: unmatched}, patched, cert, info, nil
}

func (svc *Service) PreviewReplacement(ctx context.Context, id string, req *ReplacementRequest) (*ReplacementPreview, error) {
	svc.certificateMu.Lock()
	defer svc.certificateMu.Unlock()
	preview, _, _, _, err := svc.replacement(ctx, id, req)
	return preview, err
}
func (svc *Service) ReplaceCertificate(ctx context.Context, id string, req *ReplacementRequest, requestID string) (string, error) {
	svc.certificateMu.Lock()
	defer svc.certificateMu.Unlock()
	preview, patched, cert, info, err := svc.replacement(ctx, id, req)
	if err != nil {
		return "", err
	}
	if req.ExpectedHash == "" || req.ExpectedHash != preview.FileHash || req.CertificateSHA256 != preview.CertificateSHA256 || req.DependencyHash != preview.DependencyHash {
		return "", app.NewAppError(app.ErrConflict, "配置或证书已变化，请重新预览差异", nil)
	}
	site, err := svc.siteRepo.GetByID(id)
	if err != nil || site == nil {
		return "", app.NewAppError(app.ErrConflict, "站点已变化", nil)
	}
	opID := app.NewOperationID()
	if err := svc.opRepo.Create(&repo.Operation{ID: opID, Action: "site.ssl.replace", TargetType: "site", TargetID: id, Status: "pending", RequestID: requestID, Actor: "admin", Message: "局部替换证书引用，保留原外部文件", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return "", app.NewAppError(app.ErrInternalError, "创建操作记录失败", nil)
	}
	err = svc.agent.ApplyTransaction(ctx, &TransactionRequest{OperationID: opID, TestNginx: true, ReloadNginx: true, Changes: append(preview.checks, []FileChange{
		{Type: "check", Path: cert.CertPath, ExpectedHash: info.CertSHA256},
		{Type: "check", Path: cert.KeyPath, ExpectedHash: info.KeySHA256},
		{Type: "write", Path: site.ConfigPath, ContentBase64: base64.StdEncoding.EncodeToString([]byte(patched)), ExpectedHash: req.ExpectedHash, FollowSymlink: true},
	}...)})
	if err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrAgentUnavailable, "证书引用应用未确认，请重新扫描对账", "")
		return opID, app.NewAppError(app.ErrAgentUnavailable, "证书应用失败或结果不确定；请重新扫描并确认接管以对账", map[string]any{"sync_required": true, "operation_id": opID})
	}
	ref := nginxpatch.InspectResolvedCertificate(patched, site.PrimaryDomain)
	cert.KeySHA256 = info.KeySHA256
	cert.NotBefore = &info.NotBefore
	cert.NotAfter = &info.NotAfter
	cert.Issuer = info.Issuer
	cert.Subject = info.Subject
	domainsJSON, _ := json.Marshal(info.DNSNames)
	cert.DomainsJSON = string(domainsJSON)
	if _, err := svc.certRepo.AdoptExternal(ctx, cert, id, site.ConfigPath, ref.FileHash, ref.ServerHash, opID); err != nil {
		return opID, app.NewAppError(app.ErrInternalError, "证书已应用，但关联保存失败；请重新扫描接管", map[string]any{"sync_required": true, "operation_id": opID})
	}
	if err := svc.opRepo.UpdateStatus(opID, "success"); err != nil {
		return opID, err
	}
	return opID, nil
}
