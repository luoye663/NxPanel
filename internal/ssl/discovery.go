package ssl

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

type CertificateScan struct {
	nginxpatch.CertificateReference
	configPath    string
	keyHash       string
	SiteID        string            `json:"site_id"`
	PrimaryDomain string            `json:"primary_domain"`
	Certificate   *SSLInspectResult `json:"certificate,omitempty"`
}

type AdoptCertificateRequest struct {
	SiteID       string `json:"site_id"`
	ExpectedHash string `json:"expected_hash"`
}

// ScanCertificate reads only the site's recorded configuration and delegates
// certificate/key inspection to the path-policy-protected Agent RPC.
func (svc *Service) ScanCertificate(ctx context.Context, siteID string) (*CertificateScan, error) {
	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, "读取站点失败", nil)
	}
	if site == nil {
		return nil, app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}
	content, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		if app.IsPathDeniedError(err) {
			return nil, app.NewPathDeniedError("扫描证书", "配置文件", site.ConfigPath)
		}
		return nil, app.NewAppError(app.ErrAgentUnavailable, "读取站点配置失败", nil)
	}
	ref := nginxpatch.InspectCertificate(string(content), site.PrimaryDomain)
	if resolved, _, resolveErr := svc.resolveCertificate(ctx, string(content), site.PrimaryDomain); resolveErr == nil {
		ref = resolved
	} else {
		ref.ReadOnlyReason = resolveErr.Error()
	}
	result := &CertificateScan{CertificateReference: ref, configPath: site.ConfigPath, SiteID: site.ID, PrimaryDomain: site.PrimaryDomain}
	if ref.ReadOnlyReason != "" {
		return result, nil
	}
	info, err := svc.agent.SSLInspectFiles(ctx, ref.CertPath, ref.KeyPath)
	if err != nil {
		// Do not relay Agent diagnostics (or private key bytes) to the browser.
		result.ReadOnlyReason = "证书或私钥无法校验；请检查文件、配对关系及 Agent 路径白名单"
		return result, nil
	}
	result.keyHash = info.KeySHA256
	info.KeySHA256 = ""
	result.Certificate = info
	return result, nil
}

func (svc *Service) AdoptCertificate(ctx context.Context, req *AdoptCertificateRequest, requestID string) (string, string, error) {
	svc.certificateMu.Lock()
	defer svc.certificateMu.Unlock()
	scan, err := svc.ScanCertificate(ctx, req.SiteID)
	if err != nil {
		return "", "", err
	}
	if req.ExpectedHash == "" || scan.FileHash != req.ExpectedHash {
		return "", "", app.NewAppError(app.ErrConflict, "配置已变化，请重新扫描", nil)
	}
	if scan.ReadOnlyReason != "" || scan.Certificate == nil {
		return "", "", app.NewAppError(app.ErrValidationFailed, "该配置只能只读展示，不能纳管", nil)
	}
	site, err := svc.siteRepo.GetByID(req.SiteID)
	if err != nil || site == nil || site.ConfigPath != scan.configPath || site.PrimaryDomain != scan.PrimaryDomain {
		return "", "", app.NewAppError(app.ErrConflict, "站点已变化，请重新扫描", nil)
	}
	info := scan.Certificate
	domains, _ := json.Marshal(info.DNSNames)
	c := &repo.Certificate{ID: app.NewID("cert"), Name: scan.PrimaryDomain + " (external)",
		DomainsJSON: string(domains), Issuer: info.Issuer, Subject: info.Subject,
		NotBefore: &info.NotBefore, NotAfter: &info.NotAfter, CertSHA256: info.CertSHA256,
		CertPath: scan.CertPath, KeyPath: scan.KeyPath}
	c.KeySHA256 = scan.keyHash
	opID := app.NewOperationID()
	if err := svc.opRepo.Create(&repo.Operation{ID: opID, Action: "cert.adopt", TargetType: "site", TargetID: site.ID,
		Status: "pending", RequestID: requestID, Actor: "admin", Message: "关联已有站点证书（只写元数据）",
		CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return "", "", app.NewAppError(app.ErrInternalError, "创建操作记录失败", nil)
	}
	id, err := svc.certRepo.AdoptExternal(ctx, c, site.ID, site.ConfigPath, scan.FileHash, scan.ServerHash, opID)
	if err != nil {
		_ = svc.opRepo.UpdateError(opID, "failed", app.ErrInternalError, "保存关联失败", "")
		return "", opID, app.NewAppError(app.ErrInternalError, "保存证书关联失败", nil)
	}
	if err := svc.opRepo.UpdateStatus(opID, "success"); err != nil {
		return id, opID, app.NewAppError(app.ErrInternalError, "关联已保存，但审计状态更新失败，请刷新核实", nil)
	}
	return id, opID, nil
}

func (svc *Service) managedCertificatePaths(c *repo.Certificate) bool {
	if !filepath.IsAbs(svc.cfg.Nginx.PanelDir) {
		return false
	}
	for _, path := range []string{c.CertPath, c.KeyPath} {
		owned := false
		for _, subdir := range []string{"ssl", "ssl-store"} {
			root := filepath.Join(svc.cfg.Nginx.PanelDir, subdir)
			rel, err := filepath.Rel(root, path)
			if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				owned = true
			}
		}
		if !owned {
			return false
		}
	}
	return true
}

func (svc *Service) certificateSource(c *repo.Certificate) string {
	if c.Source == "external" || !svc.managedCertificatePaths(c) {
		return "external"
	}
	return "panel"
}
