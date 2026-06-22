// rewrite 包测试 — 自定义 Location 服务业务逻辑
package rewrite

import (
	"context"
	"strings"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

// ============================================================
// mock 实现
// ============================================================

type mockSiteGetter struct {
	site *repo.Site
	err  error
}

func (m *mockSiteGetter) GetByID(id string) (*repo.Site, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.site, nil
}

type mockRewriteMeta struct {
	rw  *repo.SiteRewrite
	err error
	saved *repo.SiteRewrite
}

func (m *mockRewriteMeta) GetBySiteID(siteID string) (*repo.SiteRewrite, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.rw, nil
}

func (m *mockRewriteMeta) Upsert(sr *repo.SiteRewrite) error {
	m.saved = sr
	return nil
}

type mockOpRecorder struct {
	created  *repo.Operation
	status   string
	errID    string
	errCode  string
	errMsg   string
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
	m.errID = id
	m.errCode = errorCode
	m.errMsg = errorMessage
	return nil
}

type mockAgentTx struct {
	resp      *agentclient.TransactionResponse
	err       error
	called    bool
	lastReq   *agentclient.TransactionRequest
	readData  []byte
	readHash  string
	readErr   error
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
	return m.readData, m.readHash, nil
}

type mockTemplateStore struct {
	list        []repo.RewriteTemplate
	listErr     error
	byID        map[string]*repo.RewriteTemplate
	getErr      error
	created     *repo.RewriteTemplate
	updated     *repo.RewriteTemplate
	deletedID   string
	createErr   error
	updateErr   error
	deleteErr   error
}

func (m *mockTemplateStore) List() ([]repo.RewriteTemplate, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	if m.list == nil {
		return []repo.RewriteTemplate{}, nil
	}
	return m.list, nil
}

func (m *mockTemplateStore) ListEnabled() ([]repo.RewriteTemplate, error) {
	return m.List()
}

func (m *mockTemplateStore) GetByID(id string) (*repo.RewriteTemplate, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.byID != nil {
		if t, ok := m.byID[id]; ok {
			return t, nil
		}
	}
	return nil, nil
}

func (m *mockTemplateStore) Create(t *repo.RewriteTemplate) error {
	m.created = t
	return m.createErr
}

func (m *mockTemplateStore) Update(t *repo.RewriteTemplate) error {
	m.updated = t
	return m.updateErr
}

func (m *mockTemplateStore) Delete(id string) error {
	m.deletedID = id
	return m.deleteErr
}

// newTestService 包装 NewService，自动注入空 mockTemplateStore，保持旧用例 4 参签名
func newTestService(site siteGetter, rw rewriteMeta, op opRecorder, agent agentTx) *Service {
	return NewService(site, rw, op, agent, &mockTemplateStore{})
}

func testSite() *repo.Site {
	return &repo.Site{
		ID:            "site-001",
		PrimaryDomain: "example.com",
		DomainsJSON:   `["example.com","www.example.com"]`,
		Status:        "enabled",

		HTTPPort:      80,
		HTTPSPort:     443,
		RootPath:      "/var/www/example.com",
		IndexFiles:    "index.html index.htm",
		ConfigPath:    "/etc/nginx/sites/example.com.conf",
		RewritePath:   "/etc/nginx/rewrites/example.com.conf",
	}
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

// ============================================================
// Get 测试
// ============================================================

func TestGet_SiteNotFound(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: nil, err: nil},
		&mockRewriteMeta{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Get(context.Background(), "nonexistent")
	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND 错误，实际: %v", err)
	}
}

func TestGet_NoRewriteMeta(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{rw: nil},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	resp, err := svc.Get(context.Background(), "site-001")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if resp.Path != "/etc/nginx/rewrites/example.com.conf" {
		t.Errorf("Path 期望 rewrite path，实际: %s", resp.Path)
	}
	if resp.ContentHash != "" {
		t.Errorf("无 rewrite 时 ContentHash 应为空，实际: %s", resp.ContentHash)
	}
	if resp.SizeBytes != 0 {
		t.Errorf("无 rewrite 时 SizeBytes 应为 0，实际: %d", resp.SizeBytes)
	}
	if resp.Content != "" {
		t.Errorf("文件读取失败时 Content 应为空，实际: %s", resp.Content)
	}
}

func TestGet_WithRewriteMeta(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{rw: &repo.SiteRewrite{
			SiteID:      "site-001",
			ContentHash: "abc123",
			SizeBytes:   1024,
		}},
		&mockOpRecorder{},
		&mockAgentTx{
			readData: []byte("rewrite ^/old /new permanent;"),
			readHash: "filehash456",
		},
	)

	resp, err := svc.Get(context.Background(), "site-001")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if resp.Content != "rewrite ^/old /new permanent;" {
		t.Errorf("Content 期望文件内容，实际: %s", resp.Content)
	}
	if resp.ContentHash != "abc123" {
		t.Errorf("ContentHash 期望 abc123（DB 优先），实际: %s", resp.ContentHash)
	}
	if resp.SizeBytes != 1024 {
		t.Errorf("SizeBytes 期望 1024（DB 优先），实际: %d", resp.SizeBytes)
	}
}

// ============================================================
// Update 测试
// ============================================================

func TestUpdate_DangerNotConfirmed(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:         "rewrite ^/old /new permanent;",
		DangerConfirmed: false,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误，实际: %v", err)
	}
}

func TestUpdate_NullBytes(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:         "rewrite ^/old \x00 /new;",
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误（空字节），实际: %v", err)
	}
}

func TestUpdate_TooLarge(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	bigContent := strings.Repeat("a", 256*1024+1)
	_, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:         bigContent,
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误（超限），实际: %v", err)
	}
}

func TestUpdate_SiteNotFound(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: nil},
		&mockRewriteMeta{},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Update(context.Background(), "nonexistent", &UpdateRequest{
		Content:         "rewrite ^/old /new;",
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND 错误，实际: %v", err)
	}
}

func TestUpdate_ContentDrift(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{rw: &repo.SiteRewrite{
			SiteID:      "site-001",
			ContentHash: "old_hash",
		}},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	_, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:             "rewrite ^/old /new;",
		DangerConfirmed:     true,
		ExpectedContentHash: "wrong_hash",
	}, "req-001")

	if !isAppErr(err, app.ErrConfigDrifted) {
		t.Errorf("期望 CONFIG_DRIFTED 错误，实际: %v", err)
	}
}

func TestUpdate_AgentFailed(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{},
		&mockOpRecorder{},
		&mockAgentTx{err: context.DeadlineExceeded},
	)

	_, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:         "rewrite ^/old /new;",
		DangerConfirmed: true,
	}, "req-001")

	if !isAppErr(err, app.ErrAgentUnavailable) {
		t.Errorf("期望 AGENT_UNAVAILABLE 错误，实际: %v", err)
	}
}

func TestUpdate_Success(t *testing.T) {
	agent := &mockAgentTx{}
	opRec := &mockOpRecorder{}
	rwMeta := &mockRewriteMeta{}

	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		rwMeta,
		opRec,
		agent,
	)

	resp, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:         "rewrite ^/old /new permanent;",
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
		t.Error("agent ApplyTransaction 应被调用")
	}
	if agent.lastReq.TestNginx != true {
		t.Error("TestNginx 应为 true")
	}
	if len(agent.lastReq.Changes) != 1 {
		t.Fatal("应有 1 个文件变更")
	}
	if agent.lastReq.Changes[0].Path != "/etc/nginx/rewrites/example.com.conf" {
		t.Errorf("变更路径不正确: %s", agent.lastReq.Changes[0].Path)
	}

	// 验证 DB 元信息更新
	if rwMeta.saved == nil {
		t.Fatal("rewrite meta 应被保存")
	}
	if rwMeta.saved.ContentHash != resp.ContentHash {
		t.Errorf("保存的 hash 不匹配: %s != %s", rwMeta.saved.ContentHash, resp.ContentHash)
	}
	if rwMeta.saved.SizeBytes != len("rewrite ^/old /new permanent;") {
		t.Errorf("保存的 size 不正确: %d", rwMeta.saved.SizeBytes)
	}

	// 验证操作记录成功
	if opRec.status != "success" {
		t.Errorf("操作状态应为 success，实际: %s", opRec.status)
	}
}

func TestUpdate_DriftHashMatch(t *testing.T) {
	svc := newTestService(
		&mockSiteGetter{site: testSite()},
		&mockRewriteMeta{rw: &repo.SiteRewrite{
			SiteID:      "site-001",
			ContentHash: "correct_hash",
		}},
		&mockOpRecorder{},
		&mockAgentTx{},
	)

	// expected_content_hash 与 DB 匹配 → 不报 drift 错误
	_, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:             "rewrite ^/old /new;",
		DangerConfirmed:     true,
		ExpectedContentHash: "correct_hash",
	}, "req-001")

	if err != nil {
		t.Fatalf("hash 匹配时不应报错: %v", err)
	}
}

func TestUpdate_DisabledSite_NoReload(t *testing.T) {
	site := testSite()
	site.Status = "disabled"
	agent := &mockAgentTx{}

	svc := newTestService(
		&mockSiteGetter{site: site},
		&mockRewriteMeta{},
		&mockOpRecorder{},
		agent,
	)

	_, err := svc.Update(context.Background(), "site-001", &UpdateRequest{
		Content:         "rewrite ^/old /new;",
		DangerConfirmed: true,
	}, "req-001")

	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if agent.lastReq.ReloadNginx != false {
		t.Error("disabled 站点不应 reload nginx")
	}
}
