package acme

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/registration"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
	"github.com/luoye663/nxpanel/internal/nginx"
	"github.com/luoye663/nxpanel/internal/sse"
	"github.com/luoye663/nxpanel/internal/ssl"
)

var errPreValidationFailed = errors.New("pre-validation failed")

type ACMEAgentClient interface {
	ApplyTransaction(ctx context.Context, req *ssl.TransactionRequest) error
	ReadFile(ctx context.Context, path string) ([]byte, string, error)
	FilesWrite(ctx context.Context, path, contentBase64 string) error
	FilesRemove(ctx context.Context, paths []string) error
	FilesMkdir(ctx context.Context, path string) error
	FilesRead(ctx context.Context, path string) ([]byte, error)
}

type SSLDeployer interface {
	DeployFromStore(ctx context.Context, certID string, req *ssl.DeployFromStoreRequest, requestID string) (*ssl.SSLResponse, string, error)
}

type Service struct {
	siteRepo    *repo.SiteRepo
	sslRepo     *repo.SSLRepo
	certRepo    *repo.CertificateRepo
	acmeRepo    *repo.ACMERepo
	opRepo      *repo.OperationRepo
	agent       ACMEAgentClient
	sslDeployer SSLDeployer
	sseHub      *sse.Hub
	cfg         *app.Config
}

func NewService(
	siteRepo *repo.SiteRepo,
	sslRepo *repo.SSLRepo,
	certRepo *repo.CertificateRepo,
	acmeRepo *repo.ACMERepo,
	opRepo *repo.OperationRepo,
	agent ACMEAgentClient,
	sslDeployer SSLDeployer,
	sseHub *sse.Hub,
	cfg *app.Config,
) *Service {
	return &Service{
		siteRepo:    siteRepo,
		sslRepo:     sslRepo,
		certRepo:    certRepo,
		acmeRepo:    acmeRepo,
		opRepo:      opRepo,
		agent:       agent,
		sslDeployer: sslDeployer,
		sseHub:      sseHub,
		cfg:         cfg,
	}
}

type ACMEOrderResponse struct {
	ID                  string   `json:"id"`
	SiteID              string   `json:"site_id"`
	Domains             []string `json:"domains"`
	ChallengeType       string   `json:"challenge_type"`
	Email               string   `json:"email"`
	Status              string   `json:"status"`
	CertificateID       string   `json:"certificate_id,omitempty"`
	ErrorType           string   `json:"error_type,omitempty"`
	ErrorDetail         string   `json:"error_detail,omitempty"`
	VerificationURL     string   `json:"verification_url,omitempty"`
	VerificationContent string   `json:"verification_content,omitempty"`
	AutoRenew           bool     `json:"auto_renew"`
	ExpiresAt           string   `json:"expires_at,omitempty"`
	CreatedAt           string   `json:"created_at"`
}

type ApplyRequest struct {
	SiteID        string   `json:"site_id"`
	Domains       []string `json:"domains"`
	ChallengeType string   `json:"challenge_type"`
	Email         string   `json:"email"`
}

func orderToResp(o *repo.ACMEOrder) *ACMEOrderResponse {
	var domains []string
	if o.DomainsJSON != "" {
		json.Unmarshal([]byte(o.DomainsJSON), &domains)
	}
	resp := &ACMEOrderResponse{
		ID:                  o.ID,
		SiteID:              o.SiteID,
		Domains:             domains,
		ChallengeType:       o.ChallengeType,
		Email:               o.Email,
		Status:              o.Status,
		CertificateID:       o.CertificateID,
		ErrorType:           o.ErrorType,
		ErrorDetail:         o.ErrorDetail,
		VerificationURL:     o.VerificationURL,
		VerificationContent: o.VerificationContent,
		AutoRenew:           o.AutoRenew,
		CreatedAt:           o.CreatedAt,
	}
	if o.ExpiresAt != nil {
		resp.ExpiresAt = *o.ExpiresAt
	}
	return resp
}

func (svc *Service) ApplyCertificate(ctx context.Context, req *ApplyRequest, requestID string) (string, error) {
	if len(req.Domains) == 0 {
		return "", app.NewAppError(app.ErrValidationFailed, "请选择至少一个域名", nil)
	}
	if req.Email == "" {
		return "", app.NewAppError(app.ErrValidationFailed, "请输入邮箱", nil)
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return "", app.NewAppError(app.ErrValidationFailed, "邮箱格式不正确", nil)
	}

	site, err := svc.siteRepo.GetByID(req.SiteID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if site == nil {
		return "", app.NewAppError(app.ErrNotFound, "站点不存在", nil)
	}

	var siteDomains []string
	if site.DomainsJSON != "" {
		json.Unmarshal([]byte(site.DomainsJSON), &siteDomains)
	}
	siteDomainSet := make(map[string]bool, len(siteDomains))
	for _, d := range siteDomains {
		siteDomainSet[d] = true
	}
	for _, d := range req.Domains {
		if !siteDomainSet[d] {
			return "", app.NewAppError(app.ErrValidationFailed, fmt.Sprintf("域名 %s 不属于该站点", d), nil)
		}
	}

	if req.ChallengeType == "" {
		req.ChallengeType = "http-01"
	}
	if req.ChallengeType != "http-01" {
		return "", app.NewAppError(app.ErrValidationFailed, "当前仅支持文件验证 (http-01)", nil)
	}

	orderID := app.NewID("acme")
	domainsJSON, _ := json.Marshal(req.Domains)
	order := &repo.ACMEOrder{
		ID:            orderID,
		SiteID:        req.SiteID,
		DomainsJSON:   string(domainsJSON),
		ChallengeType: req.ChallengeType,
		Email:         req.Email,
		Status:        "pending",
		AutoRenew:     true,
	}
	if err := svc.acmeRepo.CreateOrder(order); err != nil {
		return "", app.NewAppError(app.ErrInternalError, "创建申请记录失败: "+err.Error(), nil)
	}

	go svc.doApply(orderID, req.SiteID, req.Domains, req.ChallengeType, req.Email, requestID, false)

	return orderID, nil
}

func (svc *Service) ForceObtain(orderID string) (string, error) {
	order, err := svc.acmeRepo.GetOrderByID(orderID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if order == nil {
		return "", app.NewAppError(app.ErrNotFound, "订单不存在", nil)
	}
	if order.Status != "pre_validation_failed" {
		return "", app.NewAppError(app.ErrValidationFailed, "仅预验证失败的订单可以强制提交", nil)
	}

	var domains []string
	json.Unmarshal([]byte(order.DomainsJSON), &domains)

	newOrderID := app.NewID("acme")
	newOrder := &repo.ACMEOrder{
		ID:            newOrderID,
		SiteID:        order.SiteID,
		DomainsJSON:   order.DomainsJSON,
		ChallengeType: order.ChallengeType,
		Email:         order.Email,
		Status:        "pending",
		AutoRenew:     order.AutoRenew,
	}
	if err := svc.acmeRepo.CreateOrder(newOrder); err != nil {
		return "", app.NewAppError(app.ErrInternalError, "创建申请记录失败: "+err.Error(), nil)
	}

	go svc.doApply(newOrderID, order.SiteID, domains, order.ChallengeType, order.Email, "", true)

	return newOrderID, nil
}

func (svc *Service) doApply(orderID, siteID string, domains []string, challengeType, email, requestID string, skipPreValidation bool) {
	stream := svc.sseHub.CreateStream("acme-" + orderID)
	logWriter := sse.NewLogWriter(stream, "|-")

	logWriter.Write([]byte("正在创建订单..\n"))
	svc.acmeRepo.UpdateOrderStatus(orderID, "processing", "", "")

	site, err := svc.siteRepo.GetByID(siteID)
	if err != nil || site == nil {
		svc.failOrder(orderID, "order_creation_failed", fmt.Sprintf("获取站点信息失败: %v", err), stream, logWriter)
		return
	}

	svc.acmeRepo.SaveEmail(email)

	dirURL := "https://acme-v02.api.letsencrypt.org/directory"
	if svc.cfg.ACME.UseStaging {
		dirURL = "https://acme-staging-v02.api.letsencrypt.org/directory"
	}

	logWriter.Write([]byte("正在获取/创建 ACME 账号..\n"))

	user, err := svc.getOrCreateAccount(email, dirURL)
	if err != nil {
		svc.failOrder(orderID, "account_registration_failed", err.Error(), stream, logWriter)
		return
	}

	config := lego.NewConfig(user)
	config.CADirURL = dirURL

	client, err := lego.NewClient(config)
	if err != nil {
		svc.failOrder(orderID, "order_creation_failed", "创建 ACME 客户端失败: "+err.Error(), stream, logWriter)
		return
	}

	challengeDir := filepath.Join(site.RootPath, ".well-known", "acme-challenge")
	httpProvider := &http01Provider{
		agent:             svc.agent,
		rootPath:          site.RootPath,
		challengeDir:      challengeDir,
		logWriter:         logWriter,
		skipPreValidation: skipPreValidation,
		preValConfig:      &svc.cfg.ACME.PreValidation,
	}
	client.Challenge.SetHTTP01Provider(httpProvider)

	reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
	if err != nil {
		if !strings.Contains(err.Error(), "already registered") {
			svc.failOrder(orderID, "account_registration_failed", "ACME 注册失败: "+err.Error(), stream, logWriter)
			return
		}
		reg = user.GetRegistration()
	}
	_ = reg

	logWriter.Write([]byte(fmt.Sprintf("验证目录：%s\n", challengeDir)))
	logWriter.Write([]byte("验证类型：http-01\n"))
	logWriter.Write([]byte("正在获取验证信息..\n"))

	svc.acmeRepo.UpdateOrderStatus(orderID, "verifying", "", "")

	logWriter.Write([]byte("正在验证域名..\n"))

	request := certificate.ObtainRequest{
		Domains: domains,
		Bundle:  true,
	}

	certResource, err := client.Certificate.Obtain(request)
	if err != nil {
		if errors.Is(err, errPreValidationFailed) {
			logWriter.Write([]byte("预验证失败，可在订单列表中点击「继续提交」强制申请\n"))
			svc.acmeRepo.UpdateOrderStatus(orderID, "pre_validation_failed", "pre_validation_failed", err.Error())
			logWriter.Flush()
			stream.PublishDone("|-预验证失败")
			return
		}
		errorType := classifyError(err.Error())
		verificationURL := ""
		verificationContent := ""
		svc.acmeRepo.UpdateOrderVerification(orderID, verificationURL, verificationContent)
		svc.failOrder(orderID, errorType, err.Error(), stream, logWriter)
		return
	}

	logWriter.Write([]byte("证书获取成功，正在保存..\n"))

	shortID := orderID
	if len(shortID) > 16 {
		shortID = shortID[:16]
	}
	panelDir := svc.cfg.Nginx.PanelDir
	certPath := filepath.Join(panelDir, "ssl", siteID, "le-"+shortID, "fullchain.pem")
	keyPath := filepath.Join(panelDir, "ssl", siteID, "le-"+shortID, "privkey.pem")

	changes := []ssl.FileChange{
		{
			Type:          "write",
			Path:          certPath,
			ContentBase64: base64.StdEncoding.EncodeToString(certResource.Certificate),
			Perm:          0644,
		},
		{
			Type:          "write",
			Path:          keyPath,
			ContentBase64: base64.StdEncoding.EncodeToString(certResource.PrivateKey),
			Perm:          0600,
		},
	}
	if configContent, _, readErr := svc.agent.ReadFile(context.Background(), site.ConfigPath); readErr == nil {
		markerBody := []byte(nginx.BuildACMEChallengeBlock(&nginx.RenderData{RootPath: site.RootPath}))
		patched, injectErr := nginx.EnsureMarkerBlock(configContent, nginx.MarkerNameACMEChallenge, markerBody)
		if injectErr != nil {
			svc.failOrder(orderID, "config_update_failed", "注入 ACME 标识块失败: "+injectErr.Error(), stream, logWriter)
			return
		}
		changes = append(changes, ssl.FileChange{
			Type:          "write",
			Path:          site.ConfigPath,
			ContentBase64: base64.StdEncoding.EncodeToString(patched),
			Perm:          0644,
		})
	} else {
		svc.failOrder(orderID, "config_read_failed", "读取站点配置失败: "+readErr.Error(), stream, logWriter)
		return
	}

	opID := app.NewOperationID()
	svc.opRepo.Create(&repo.Operation{
		ID: opID, Action: "acme.obtain", TargetType: "site", TargetID: siteID,
		Status: "pending", RequestID: requestID, Actor: "admin",
		Message:   fmt.Sprintf("Let's Encrypt 申请证书: %s", strings.Join(domains, ", ")),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	})

	agentErr := svc.agent.ApplyTransaction(context.Background(), &ssl.TransactionRequest{
		OperationID: opID,
		Changes:     changes,
		TestNginx:   true,
		ReloadNginx: site.Status == "enabled",
	})
	if agentErr != nil {
		svc.failOrder(orderID, "certificate_download_failed", "写入证书文件失败: "+agentErr.Error(), stream, logWriter)
		return
	}

	inspectResp, err := ssl.InspectPair(certResource.Certificate, certResource.PrivateKey)
	if err == nil {
		dnsNamesJSON, _ := json.Marshal(inspectResp.DNSNames)
		cert := &repo.Certificate{
			ID:          app.NewID("cert"),
			Name:        site.PrimaryDomain,
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
		if createErr := svc.certRepo.Create(cert); createErr != nil {
			slog.Warn("保存到证书夹失败", "error", createErr)
		} else {
			svc.acmeRepo.UpdateOrderSuccess(orderID, cert.ID, inspectResp.NotAfter)
			svc.opRepo.UpdateStatus(opID, "success")

			logWriter.Write([]byte("证书已保存到证书夹，正在自动部署..\n"))
			_, _, deployErr := svc.sslDeployer.DeployFromStore(context.Background(), cert.ID, &ssl.DeployFromStoreRequest{
				SiteID:     siteID,
				ForceHTTPS: true,
			}, requestID)
			if deployErr != nil {
				slog.Warn("自动部署失败", "error", deployErr)
				logWriter.Write([]byte("自动部署失败，请手动部署\n"))
			}
		}
	}

	svc.opRepo.UpdateStatus(opID, "success")

	httpProvider.cleanAll()

	logWriter.Flush()
	stream.PublishDone("|-申请完成")
}

func (svc *Service) failOrder(orderID, errorType, errorDetail string, stream *sse.Stream, logWriter *sse.LogWriter) {
	slog.Error("ACME 申请失败", "order_id", orderID, "error_type", errorType, "error_detail", errorDetail)
	svc.acmeRepo.UpdateOrderStatus(orderID, "failed", errorType, errorDetail)
	logWriter.Flush()
	stream.PublishDone("|-申请失败")
}

func resolveDNS(domain, dnsServer string, logWriter *sse.LogWriter) []net.IPAddr {
	if strings.HasPrefix(dnsServer, "https://") {
		return resolveDoH(domain, dnsServer, logWriter)
	}

	var resolver *net.Resolver
	if dnsServer != "" {
		resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				return net.Dial("udp", dnsServer)
			},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var allIPs []net.IPAddr
	ips, err := resolver.LookupIPAddr(ctx, domain)
	if err == nil {
		allIPs = append(allIPs, ips...)
	} else {
		logWriter.Write([]byte(fmt.Sprintf("  DNS 查询失败：%v\n", err)))
	}

	return allIPs
}

type dohAnswer struct {
	Data string `json:"data"`
}

type dohResponse struct {
	Status int         `json:"Status"`
	Answer []dohAnswer `json:"Answer"`
}

func resolveDoH(domain, dohURL string, logWriter *sse.LogWriter) []net.IPAddr {
	var allIPs []net.IPAddr

	for _, qtype := range []string{"A", "AAAA"} {
		u, _ := url.Parse(dohURL)
		qs := u.Query()
		qs.Set("name", domain)
		qs.Set("type", qtype)
		u.RawQuery = qs.Encode()

		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("GET", u.String(), nil)
		if err != nil {
			logWriter.Write([]byte(fmt.Sprintf("  DoH %s 请求构建失败：%v\n", qtype, err)))
			continue
		}
		req.Header.Set("Accept", "application/dns-json")

		resp, err := client.Do(req)
		if err != nil {
			logWriter.Write([]byte(fmt.Sprintf("  DoH %s 查询失败：%v\n", qtype, err)))
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		resp.Body.Close()

		if resp.StatusCode != 200 {
			logWriter.Write([]byte(fmt.Sprintf("  DoH %s 返回状态 %d\n", qtype, resp.StatusCode)))
			continue
		}

		var dohResp dohResponse
		if err := json.Unmarshal(body, &dohResp); err != nil {
			logWriter.Write([]byte(fmt.Sprintf("  DoH %s 响应解析失败：%v\n", qtype, err)))
			continue
		}

		for _, ans := range dohResp.Answer {
			ip := net.ParseIP(ans.Data)
			if ip != nil {
				allIPs = append(allIPs, net.IPAddr{IP: ip})
			}
		}
	}

	return allIPs
}

func httpLocalhostCheck(domain, token, expectedContent string) error {
	checkURL := fmt.Sprintf("http://127.0.0.1/.well-known/acme-challenge/%s", token)
	req, err := http.NewRequest("GET", checkURL, nil)
	if err != nil {
		return fmt.Errorf("构建请求失败: %w", err)
	}
	req.Host = domain

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("本地 HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("本地 HTTP 状态码 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	bodyStr := strings.TrimSpace(string(body))
	if bodyStr != expectedContent {
		return fmt.Errorf("响应内容不匹配（期望 %d 字节，实际 %d 字节）", len(expectedContent), len(bodyStr))
	}

	return nil
}

func classifyError(errMsg string) string {
	lower := strings.ToLower(errMsg)
	if strings.Contains(lower, "nxdomain") || strings.Contains(lower, "dns") {
		return "dns_resolution_failed"
	}
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline") {
		return "connection_timeout"
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many") {
		return "rate_limited"
	}
	if strings.Contains(lower, "challenge") || strings.Contains(lower, "validation") {
		return "challenge_failed"
	}
	return "unknown"
}

func (svc *Service) getOrCreateAccount(email, dirURL string) (*acmeUser, error) {
	existing, _ := svc.acmeRepo.GetAccountByEmail(email)

	if existing != nil {
		block, _ := pem.Decode([]byte(existing.PrivateKeyPEM))
		if block == nil {
			return nil, fmt.Errorf("解析已有 ACME 账号私钥失败")
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("解析已有 ACME 账号私钥失败: %w", err)
		}
		return &acmeUser{email: email, key: key, dirURL: dirURL}, nil
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("生成 ACME 账号密钥失败: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	account := &repo.ACMEAccount{
		ID:            app.NewID("acme"),
		Email:         email,
		PrivateKeyPEM: string(keyPEM),
		DirectoryURL:  dirURL,
	}
	if err := svc.acmeRepo.CreateAccount(account); err != nil {
		return nil, fmt.Errorf("保存 ACME 账号失败: %w", err)
	}

	return &acmeUser{email: email, key: privateKey, dirURL: dirURL}, nil
}

type acmeUser struct {
	email  string
	key    *rsa.PrivateKey
	dirURL string
	reg    *registration.Resource
}

func (u *acmeUser) GetEmail() string                        { return u.email }
func (u *acmeUser) GetRegistration() *registration.Resource { return u.reg }
func (u *acmeUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

type http01Provider struct {
	agent             ACMEAgentClient
	rootPath          string
	challengeDir      string
	logWriter         *sse.LogWriter
	writtenFiles      []string
	skipPreValidation bool
	preValConfig      *app.PreValidationConfig
}

func (p *http01Provider) Present(domain, token, keyAuth string) error {
	if err := p.agent.FilesMkdir(context.Background(), p.challengeDir); err != nil {
		return fmt.Errorf("创建验证目录失败: %w", err)
	}

	filePath := filepath.Join(p.challengeDir, token)
	content := base64.StdEncoding.EncodeToString([]byte(keyAuth))
	if err := p.agent.FilesWrite(context.Background(), filePath, content); err != nil {
		return fmt.Errorf("写入验证文件失败: %w", err)
	}
	p.writtenFiles = append(p.writtenFiles, filePath)

	p.logWriter.Write([]byte(fmt.Sprintf("已写入验证文件：%s\n", filePath)))

	if p.skipPreValidation {
		return nil
	}

	return p.runPreValidation(domain, token, keyAuth)
}

func (p *http01Provider) runPreValidation(domain, token, keyAuth string) error {
	pv := p.preValConfig
	retryInterval := 3 * time.Second
	if pv.RetryInterval != "" {
		if d, err := time.ParseDuration(pv.RetryInterval); err == nil {
			retryInterval = d
		}
	}
	retryCount := pv.RetryCount
	if retryCount <= 0 {
		retryCount = 5
	}

	p.logWriter.Write([]byte(fmt.Sprintf("===== 预验证域名 %s =====\n", domain)))

	ips := resolveDNS(domain, pv.DNSServer, p.logWriter)
	if len(ips) == 0 {
		p.logWriter.Write([]byte(fmt.Sprintf("  ✗ DNS 解析：%s 未解析到任何 IP，ACME 验证无法完成\n", domain)))
		return fmt.Errorf("%w: 域名 %s DNS 未解析，无法完成 ACME 验证", errPreValidationFailed, domain)
	}
	ipStrs := make([]string, len(ips))
	for i, ip := range ips {
		ipStrs[i] = ip.String()
	}
	p.logWriter.Write([]byte(fmt.Sprintf("  DNS 解析：%s → [%s]\n", domain, strings.Join(ipStrs, ", "))))

	p.logWriter.Write([]byte(fmt.Sprintf("  本地验证：http://127.0.0.1/.well-known/acme-challenge/%s (Host: %s)\n", token, domain)))

	ok := false
	var lastErr error
	for attempt := 1; attempt <= retryCount; attempt++ {
		if attempt > 1 {
			p.logWriter.Write([]byte(fmt.Sprintf("  等待 %s 后重试（第 %d/%d 次）...\n", retryInterval, attempt, retryCount)))
			time.Sleep(retryInterval)
		}

		lastErr = httpLocalhostCheck(domain, token, keyAuth)
		if lastErr == nil {
			p.logWriter.Write([]byte(fmt.Sprintf("  本地验证第 %d 次尝试成功 ✓\n", attempt)))
			ok = true
			break
		}
		p.logWriter.Write([]byte(fmt.Sprintf("  本地验证第 %d 次尝试失败：%s\n", attempt, lastErr.Error())))
	}

	if !ok {
		return fmt.Errorf("%w: 域名 %s 本地验证失败（%d 次重试后仍无法访问验证文件）：%s", errPreValidationFailed, domain, retryCount, lastErr)
	}

	p.tryPublicHTTPCheck(domain, token, keyAuth, retryInterval, retryCount)

	p.logWriter.Write([]byte(fmt.Sprintf("  预验证通过 ✓\n")))

	return nil
}

func (p *http01Provider) tryPublicHTTPCheck(domain, token, keyAuth string, retryInterval time.Duration, retryCount int) {
	checkURL := fmt.Sprintf("http://%s/.well-known/acme-challenge/%s", domain, token)
	p.logWriter.Write([]byte(fmt.Sprintf("  公网验证：%s\n", checkURL)))

	client := &http.Client{Timeout: 10 * time.Second}

	for attempt := 1; attempt <= retryCount; attempt++ {
		if attempt > 1 {
			p.logWriter.Write([]byte(fmt.Sprintf("  公网验证等待 %s 后重试（第 %d/%d 次）...\n", retryInterval, attempt, retryCount)))
			time.Sleep(retryInterval)
		}

		req, err := http.NewRequest("GET", checkURL, nil)
		if err != nil {
			p.logWriter.Write([]byte("  ⚠ 公网验证请求构建失败\n"))
			return
		}

		resp, err := client.Do(req)
		if err != nil {
			p.logWriter.Write([]byte(fmt.Sprintf("  公网验证第 %d 次尝试失败：%s\n", attempt, err.Error())))
			continue
		}

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()

		if resp.StatusCode != 200 {
			p.logWriter.Write([]byte(fmt.Sprintf("  公网验证第 %d 次尝试返回状态码 %d\n", attempt, resp.StatusCode)))
			continue
		}

		if strings.TrimSpace(string(body)) != keyAuth {
			p.logWriter.Write([]byte(fmt.Sprintf("  公网验证第 %d 次尝试响应内容不匹配\n", attempt)))
			continue
		}

		p.logWriter.Write([]byte(fmt.Sprintf("  公网验证第 %d 次尝试成功 ✓\n", attempt)))
		return
	}

	p.logWriter.Write([]byte("  ⚠ 公网验证多次尝试失败（可能是 NAT 回流问题），但本地验证已通过，ACME 验证通常仍可成功\n"))
}

func (p *http01Provider) CleanUp(domain, token, keyAuth string) error {
	if len(p.writtenFiles) == 0 {
		return nil
	}
	return p.agent.FilesRemove(context.Background(), p.writtenFiles)
}

func (p *http01Provider) cleanAll() {
	if len(p.writtenFiles) > 0 {
		p.agent.FilesRemove(context.Background(), p.writtenFiles)
	}
}

func (svc *Service) ListOrders(siteID string) ([]*ACMEOrderResponse, error) {
	orders, err := svc.acmeRepo.ListOrdersBySiteID(siteID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	result := make([]*ACMEOrderResponse, 0, len(orders))
	for _, o := range orders {
		result = append(result, orderToResp(o))
	}
	return result, nil
}

func (svc *Service) GetOrder(orderID string) (*ACMEOrderResponse, error) {
	order, err := svc.acmeRepo.GetOrderByID(orderID)
	if err != nil {
		return nil, app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if order == nil {
		return nil, app.NewAppError(app.ErrNotFound, "订单不存在", nil)
	}
	return orderToResp(order), nil
}

func (svc *Service) StreamLogs(orderID string) *sse.Stream {
	return svc.sseHub.GetStream("acme-" + orderID)
}

func (svc *Service) RenewOrder(ctx context.Context, orderID, requestID string) (string, error) {
	order, err := svc.acmeRepo.GetOrderByID(orderID)
	if err != nil {
		return "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if order == nil {
		return "", app.NewAppError(app.ErrNotFound, "订单不存在", nil)
	}

	var domains []string
	json.Unmarshal([]byte(order.DomainsJSON), &domains)

	newOrderID, err := svc.ApplyCertificate(ctx, &ApplyRequest{
		SiteID:        order.SiteID,
		Domains:       domains,
		ChallengeType: order.ChallengeType,
		Email:         order.Email,
	}, requestID)
	if err != nil {
		return "", err
	}

	svc.acmeRepo.UpdateOrderRenewed(orderID)
	return newOrderID, nil
}

func (svc *Service) DeleteOrder(ctx context.Context, orderID, requestID string) error {
	order, err := svc.acmeRepo.GetOrderByID(orderID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if order == nil {
		return app.NewAppError(app.ErrNotFound, "订单不存在", nil)
	}

	if err := svc.acmeRepo.DeleteOrder(orderID); err != nil {
		return app.NewAppError(app.ErrInternalError, "删除失败: "+err.Error(), nil)
	}
	return nil
}

func (svc *Service) DownloadOrder(ctx context.Context, orderID string) ([]byte, string, error) {
	order, err := svc.acmeRepo.GetOrderByID(orderID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if order == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "订单不存在", nil)
	}
	if order.Status != "success" || order.CertificateID == "" {
		return nil, "", app.NewAppError(app.ErrValidationFailed, "该订单无有效证书", nil)
	}

	cert, err := svc.certRepo.GetByID(order.CertificateID)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if cert == nil {
		return nil, "", app.NewAppError(app.ErrNotFound, "证书记录不存在", nil)
	}

	certPEM, err := svc.agent.FilesRead(ctx, cert.CertPath)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "读取证书文件失败: "+err.Error(), nil)
	}
	keyPEM, err := svc.agent.FilesRead(ctx, cert.KeyPath)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "读取私钥文件失败: "+err.Error(), nil)
	}

	zipBytes, err := createCertZIP(certPEM, keyPEM)
	if err != nil {
		return nil, "", app.NewAppError(app.ErrInternalError, "打包失败: "+err.Error(), nil)
	}

	var domains []string
	json.Unmarshal([]byte(order.DomainsJSON), &domains)
	filename := "certificate.zip"
	if len(domains) > 0 {
		filename = domains[0] + "_ssl.zip"
	}

	return zipBytes, filename, nil
}

func (svc *Service) DeployOrder(ctx context.Context, orderID, requestID string) error {
	order, err := svc.acmeRepo.GetOrderByID(orderID)
	if err != nil {
		return app.NewAppError(app.ErrInternalError, err.Error(), nil)
	}
	if order == nil {
		return app.NewAppError(app.ErrNotFound, "订单不存在", nil)
	}
	if order.Status != "success" || order.CertificateID == "" {
		return app.NewAppError(app.ErrValidationFailed, "该订单无有效证书", nil)
	}

	_, _, err = svc.sslDeployer.DeployFromStore(ctx, order.CertificateID, &ssl.DeployFromStoreRequest{
		SiteID:     order.SiteID,
		ForceHTTPS: true,
	}, requestID)
	if err != nil {
		return err
	}
	return nil
}

func (svc *Service) SetAutoRenew(orderID string, enabled bool) error {
	return svc.acmeRepo.UpdateOrderAutoRenew(orderID, enabled)
}

func (svc *Service) ListEmails() ([]string, error) {
	return svc.acmeRepo.ListEmails()
}

func (svc *Service) SaveEmail(email string) error {
	return svc.acmeRepo.SaveEmail(email)
}

func (svc *Service) DeleteEmail(email string) error {
	return svc.acmeRepo.DeleteEmail(email)
}

func createCertZIP(certPEM, keyPEM []byte) ([]byte, error) {
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
