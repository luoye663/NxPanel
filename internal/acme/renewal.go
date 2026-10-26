package acme

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/sse"
	"github.com/luoye663/nxpanel/internal/ssl"
)

type applyOptions struct {
	previousOrderID string
	autoRenew       *bool
	taskCtx         context.Context
	done            chan error
}

func (svc *Service) admitSite(siteID string) bool {
	svc.jobsMu.Lock()
	defer svc.jobsMu.Unlock()
	if svc.jobsClosing || svc.rootCtx.Err() != nil || svc.activeSites[siteID] {
		return false
	}
	select {
	case svc.jobSlots <- struct{}{}:
		if svc.activeSites == nil {
			svc.activeSites = make(map[string]bool)
		}
		svc.activeSites[siteID] = true
		svc.jobsWG.Add(1)
		return true
	default:
		return false
	}
}

func (svc *Service) releaseSite(siteID string) {
	svc.jobsMu.Lock()
	delete(svc.activeSites, siteID)
	svc.jobsMu.Unlock()
	svc.releaseJob()
}

func (svc *Service) prepareChallengeConfig(ctx context.Context, site *repo.Site, requestID string) error {
	if site.Status != "enabled" {
		return fmt.Errorf("站点未启用，无法执行 HTTP-01 验证")
	}
	current, _, err := svc.agent.ReadFile(ctx, site.ConfigPath)
	if err != nil {
		return fmt.Errorf("读取站点配置失败: %w", err)
	}
	patched, err := nginx.EnsureMarkerBlock(current, nginx.MarkerNameACMEChallenge, []byte(nginx.BuildACMEChallengeBlock(&nginx.RenderData{RootPath: site.RootPath})))
	if err != nil {
		return err
	}
	// Only replace managed redirect blocks; custom server rewrites remain user-owned.
	if bytes.Contains(current, []byte(nginx.MarkerForceHTTPSStart)) || bytes.Contains(current, []byte(nginx.MarkerForceHTTPSEnd)) {
		body, err := nginx.ExtractMarkerBlock(current, nginx.MarkerNameForceHTTPS)
		if err != nil {
			return err
		}
		if strings.TrimSpace(string(body)) != "" {
			patched, err = nginx.ReplaceMarkerBlock(patched, nginx.MarkerNameForceHTTPS, []byte(nginx.BuildForceHTTPSBlock(&nginx.RenderData{SSL: &nginx.SSLData{Enabled: true, ForceHTTPS: true}})))
			if err != nil {
				return err
			}
		}
	}
	if bytes.Equal(current, patched) {
		return nil
	}
	opID := app.NewOperationID()
	if err := svc.opRepo.Create(&repo.Operation{ID: opID, Action: "acme.prepare", TargetType: "site", TargetID: site.ID, Status: "pending", Actor: "admin", RequestID: requestID, Message: "准备 HTTP-01 验证配置", CreatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		return err
	}
	err = svc.agent.ApplyTransaction(ctx, &ssl.TransactionRequest{OperationID: opID, Changes: []ssl.FileChange{{Type: "write", Path: site.ConfigPath, ContentBase64: base64.StdEncoding.EncodeToString(patched), Perm: 0644}}, TestNginx: true, ReloadNginx: true})
	if err != nil {
		svc.opRepo.UpdateError(opID, "failed", "config_update_failed", err.Error(), "")
		return fmt.Errorf("应用 HTTP-01 验证配置失败: %w", err)
	}
	return svc.opRepo.UpdateStatus(opID, "success")
}

func newPreValidationHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func validationStatusError(resp *http.Response) error {
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return fmt.Errorf("HTTP 验证路径发生重定向 (%d, Location: %s)，请检查强制 HTTPS、自定义跳转或 CDN 规则", resp.StatusCode, resp.Header.Get("Location"))
	}
	return fmt.Errorf("HTTP 验证状态码 %d", resp.StatusCode)
}

func (p *http01Provider) cleanupFiles() error {
	if len(p.writtenFiles) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(p.ctx), 10*time.Second)
	defer cancel()
	if err := p.agent.FilesRemove(ctx, p.writtenFiles); err != nil {
		return err
	}
	p.writtenFiles = nil
	return nil
}

func (svc *Service) saveAndDeploy(ctx context.Context, orderID, siteID, requestID, opID, certPath, keyPath, name string, resource *certificate.Resource, stream *sse.Stream, logWriter *sse.LogWriter) error {
	fail := func(kind string, err error) error {
		svc.opRepo.UpdateError(opID, "failed", kind, err.Error(), "")
		svc.failOrder(orderID, kind, err.Error(), stream, logWriter)
		return err
	}
	inspected, err := ssl.InspectPair(resource.Certificate, resource.PrivateKey)
	if err != nil {
		return fail("certificate_invalid", err)
	}
	domains, err := json.Marshal(inspected.DNSNames)
	if err != nil {
		return fail("certificate_invalid", err)
	}
	cert := &repo.Certificate{ID: app.NewID("cert"), Name: name, DomainsJSON: string(domains), Issuer: inspected.Issuer, Subject: inspected.Subject, NotBefore: &inspected.NotBefore, NotAfter: &inspected.NotAfter, CertSHA256: inspected.CertSHA256, KeySHA256: inspected.KeySHA256, CertPath: certPath, KeyPath: keyPath}
	if err := svc.certRepo.Create(cert); err != nil {
		return fail("certificate_save_failed", err)
	}
	if err := svc.acmeRepo.UpdateOrderIssued(orderID, cert.ID, inspected.NotAfter); err != nil {
		return fail("database_error", err)
	}
	deployErr := svc.deployIssuedCertificate(ctx, siteID, cert.ID, requestID)
	if deployErr != nil {
		err := fmt.Errorf("证书已保存，但自动部署失败，请手动部署: %w", deployErr)
		svc.acmeRepo.UpdateOrderStatus(orderID, "success", "deployment_failed", err.Error())
		svc.opRepo.UpdateError(opID, "failed", "deployment_failed", err.Error(), "")
		logWriter.Write([]byte(err.Error() + "\n"))
		logWriter.Flush()
		stream.PublishDone("|-证书已签发，部署失败")
		return err
	}
	if err := svc.opRepo.UpdateStatus(opID, "success"); err != nil {
		return fail("database_error", err)
	}
	if err := svc.acmeRepo.UpdateOrderStatus(orderID, "success", "", ""); err != nil {
		return fail("database_error", err)
	}
	logWriter.Write([]byte("证书已保存并成功部署\n"))
	logWriter.Flush()
	return nil
}

func (svc *Service) deployIssuedCertificate(ctx context.Context, siteID, certID, requestID string) error {
	row, err := svc.sslRepo.GetBySiteID(siteID)
	if err != nil {
		return err
	}
	req := &ssl.DeployFromStoreRequest{SiteID: siteID, ForceHTTPS: true}
	if row != nil && row.Enabled {
		req.ForceHTTPS = row.ForceHTTPS
		req.HSTSEnabled = row.HSTSEnabled
	}
	_, _, err = svc.sslDeployer.DeployFromStore(ctx, certID, req, requestID)
	return err
}
