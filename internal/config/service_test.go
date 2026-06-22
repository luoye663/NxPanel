// config 包测试 — 完整站点配置编辑业务逻辑
package config

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// ============================================================
// mock 实现
// ============================================================

type mockSiteStore struct {
	site  *repo.Site
	err   error
	saved *repo.Site
}

func (m *mockSiteStore) GetByID(id string) (*repo.Site, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.site, nil
}

func (m *mockSiteStore) Update(s *repo.Site) error {
	m.saved = s
	return nil
}

type mockProxyGetter struct {
	proxy *repo.SiteProxy
	err   error
}

func (m *mockProxyGetter) GetBySiteID(siteID string) (*repo.SiteProxy, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.proxy, nil
}

type mockSSLGetter struct {
	ssl *repo.SiteSSL
	err error
}

func (m *mockSSLGetter) GetBySiteID(siteID string) (*repo.SiteSSL, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.ssl, nil
}

type mockOpRecorder struct {
	created *repo.Operation
	status  string
}

func (m *mockOpRecorder) Create(o *repo.Operation) error {
	m.created = o
	return nil
}

func (m *mockOpRecorder) UpdateStatus(id, status string) error {
	m.status = status
	return nil
}

func (m *mockOpRecorder) UpdateError(id, status, errorCode, errorMessage, stderr string) error {
	return nil
}

type mockAgentTx struct {
	resp        *agentclient.TransactionResponse
	err         error
	called      bool
	lastReq     *agentclient.TransactionRequest
	readContent []byte
	readHash    string
	readErr     error
}

func (m *mockAgentTx) ApplyTransaction(ctx context.Context, req *agentclient.TransactionRequest) (*agentclient.TransactionResponse, error) {
	m.called = true
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	if m.resp != nil {
		return m.resp, nil
	}
	return &agentclient.TransactionResponse{}, nil
}

func (m *mockAgentTx) ReadFile(ctx context.Context, path string) ([]byte, string, error) {
	if m.readErr != nil {
		return nil, "", m.readErr
	}
	return m.readContent, m.readHash, nil
}

func isAppErr(err error, code string) bool {
	if err == nil {
		return false
	}
	if ae, ok := err.(*app.AppError); ok {
		return ae.Code == code
	}
	return false
}

func testSite() *repo.Site {
	return &repo.Site{
		ID:            "site-001",
		PrimaryDomain: "example.com",
		DomainsJSON:   `["example.com","www.example.com"]`,
		Status:        "enabled",

		HTTPPort:         80,
		HTTPSPort:        443,
		RootPath:         "/var/www/example.com",
		IndexFiles:       "index.html index.htm",
		AccessLogEnabled: true,
		AccessLogPath:    "/var/log/nginx/example.com.access.log",
		ErrorLogPath:     "/var/log/nginx/example.com.error.log",
		ConfigPath:       "/etc/nginx/sites/example.com.conf",
		EnabledPath:      "/etc/nginx/enabled/example.com.conf",
		RewritePath:      "/etc/nginx/rewrites/example.com.conf",
	}
}

func validConfig(siteID string) string {
	return fmt.Sprintf(`#NXPANEL-SITE-START site_id=%s
server {
    listen 80;
    server_name example.com www.example.com;
    root /var/www/example.com;
    index index.html index.htm;
}
#NXPANEL-SITE-END`, siteID)
}

// ============================================================
// validateSiteMarkers 测试
// ============================================================

func TestValidateSiteMarkers_MissingStart(t *testing.T) {
	err := validateSiteMarkers("server { }", "site-001")
	if err == nil {
		t.Fatal("缺少 SITE-START marker 应报错")
	}
	if !strings.Contains(err.Error(), "NXPANEL-SITE-START") {
		t.Errorf("错误信息不正确: %v", err)
	}
}

func TestValidateSiteMarkers_MissingEnd(t *testing.T) {
	content := "#NXPANEL-SITE-START site_id=site-001\nserver { }"
	err := validateSiteMarkers(content, "site-001")
	if err == nil {
		t.Fatal("缺少 SITE-END marker 应报错")
	}
	if !strings.Contains(err.Error(), "NXPANEL-SITE-END") {
		t.Errorf("错误信息不正确: %v", err)
	}
}

func TestValidateSiteMarkers_SiteIDMismatch(t *testing.T) {
	content := "#NXPANEL-SITE-START site_id=other-site\nserver { }\n#NXPANEL-SITE-END"
	err := validateSiteMarkers(content, "site-001")
	if err == nil {
		t.Fatal("site_id 不匹配应报错")
	}
	if !strings.Contains(err.Error(), "site_id 不匹配") {
		t.Errorf("错误信息不正确: %v", err)
	}
}

func TestValidateSiteMarkers_Valid(t *testing.T) {
	content := validConfig("site-001")
	err := validateSiteMarkers(content, "site-001")
	if err != nil {
		t.Fatalf("有效的 marker 不应报错: %v", err)
	}
}

// ============================================================
// Get 测试
// ============================================================

func TestGet_SiteNotFound(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: nil},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Get("nonexistent")
	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND 错误，实际: %v", err)
	}
}

func TestGet_Success(t *testing.T) {
	site := testSite()
	svc := NewService(
		&mockSiteStore{site: site},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{readContent: []byte("server { }"), readHash: "rendered_hash_abc"},
	)

	resp, err := svc.Get("site-001")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if resp.ContentHash != "rendered_hash_abc" {
		t.Errorf("ContentHash 不正确: %s", resp.ContentHash)
	}
}

// ============================================================
// Save 测试
// ============================================================

func TestSave_DangerNotConfirmed(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: testSite()},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         validConfig("site-001"),
		DangerConfirmed: false,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误，实际: %v", err)
	}
}

func TestSave_NullBytes(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: testSite()},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	content := strings.ReplaceAll(validConfig("site-001"), "server", "server\x00")
	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         content,
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误（空字节），实际: %v", err)
	}
}

func TestSave_TooLarge(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: testSite()},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	bigContent := strings.Repeat("a", 512*1024+1)
	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         bigContent,
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误（超限），实际: %v", err)
	}
}

func TestSave_MissingMarker(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: testSite()},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         "server { listen 80; }",
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误（缺少 marker），实际: %v", err)
	}
}

func TestSave_SiteIDMismatch(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: testSite()},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	// marker 中 site_id=wrong-id，与 site-001 不匹配
	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         validConfig("wrong-id"),
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误（site_id 不匹配），实际: %v", err)
	}
}

func TestSave_ContentDrift(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: testSite()},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:             validConfig("site-001"),
		DangerConfirmed:     true,
		ExpectedContentHash: "wrong_hash",
	}, "req-001")

	if !isAppErr(err, app.ErrConfigDrifted) {
		t.Errorf("期望 CONFIG_DRIFTED 错误，实际: %v", err)
	}
}

func TestSave_AgentFailed(t *testing.T) {
	svc := NewService(
		&mockSiteStore{site: testSite()},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		&mockAgentTx{err: context.DeadlineExceeded},
	)

	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         validConfig("site-001"),
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrAgentUnavailable) {
		t.Errorf("期望 AGENT_UNAVAILABLE 错误，实际: %v", err)
	}
}

func TestSave_Success(t *testing.T) {
	agent := &mockAgentTx{}
	siteStore := &mockSiteStore{site: testSite()}
	opRec := &mockOpRecorder{}

	svc := NewService(
		siteStore,
		&mockProxyGetter{},
		&mockSSLGetter{},
		opRec,
		agent,
	)

	resp, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         validConfig("site-001"),
		DangerConfirmed: true,
	}, "req-001")

	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	if resp.ContentHash == "" {
		t.Error("ContentHash 不应为空")
	}
	if resp.OperationID == "" {
		t.Error("OperationID 不应为空")
	}

	// 验证 agent 被调用
	if !agent.called {
		t.Error("agent 应被调用")
	}
	if agent.lastReq.TestNginx != true {
		t.Error("TestNginx 应为 true")
	}

	// 验证 DB 更新
	if siteStore.saved == nil {
		t.Fatal("站点应被更新")
	}
	if siteStore.saved.PrimaryDomain != "example.com" {
		t.Error("站点主要域名应保持不变")
	}

	// 操作记录成功
	if opRec.status != "success" {
		t.Errorf("操作状态应为 success，实际: %s", opRec.status)
	}
}

func TestSave_DisabledSite_NoReload(t *testing.T) {
	site := testSite()
	site.Status = "disabled"
	agent := &mockAgentTx{}

	svc := NewService(
		&mockSiteStore{site: site},
		&mockProxyGetter{},
		&mockSSLGetter{},
		&mockOpRecorder{},
		agent,
	)

	_, err := svc.Save(context.Background(), "site-001", &SaveRequest{
		Content:         validConfig("site-001"),
		DangerConfirmed: true,
	}, "req-001")

	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if agent.lastReq.ReloadNginx != false {
		t.Error("disabled 站点不应 reload nginx")
	}
}
