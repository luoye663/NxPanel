// logs 包测试 — 日志服务业务逻辑
package logs

import (
	"context"
	"net/http"
	"testing"

	"github.com/luoye663/nxpanel/internal/agentclient"
	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/db/repo"
)

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

type mockAgentCaller struct {
	tailResp  *agentclient.LogTailResponse
	tailErr   error
	truncResp *agentclient.LogTruncateResponse
	truncErr  error
}

func (m *mockAgentCaller) LogTail(ctx context.Context, req *agentclient.LogTailRequest) (*agentclient.LogTailResponse, error) {
	if m.tailErr != nil {
		return nil, m.tailErr
	}
	if m.tailResp != nil {
		return m.tailResp, nil
	}
	return &agentclient.LogTailResponse{Lines: []string{}, Truncated: false}, nil
}

func (m *mockAgentCaller) LogTruncate(ctx context.Context, req *agentclient.LogTruncateRequest) (*agentclient.LogTruncateResponse, error) {
	if m.truncErr != nil {
		return nil, m.truncErr
	}
	return &agentclient.LogTruncateResponse{OK: true}, nil
}

func (m *mockAgentCaller) LogSearch(ctx context.Context, req *agentclient.LogSearchRequest) (*agentclient.LogSearchResponse, error) {
	return &agentclient.LogSearchResponse{Lines: []string{}, Matched: 0, Truncated: false, MaxBytes: req.MaxBytes}, nil
}

func (m *mockAgentCaller) RotatedLogList(ctx context.Context, req *agentclient.RotatedLogListRequest) (*agentclient.RotatedLogListResponse, error) {
	return &agentclient.RotatedLogListResponse{Items: []agentclient.RotatedLogItem{}}, nil
}

func (m *mockAgentCaller) RotatedLogTail(ctx context.Context, req *agentclient.RotatedLogTailRequest) (*agentclient.LogTailResponse, error) {
	return &agentclient.LogTailResponse{Lines: []string{}, Truncated: false}, nil
}

func (m *mockAgentCaller) RotatedLogRemove(ctx context.Context, req *agentclient.RotatedLogRemoveRequest) error {
	return nil
}

func (m *mockAgentCaller) LogDownload(ctx context.Context, path string) (*http.Response, error) {
	return &http.Response{}, nil
}

func testLogSite() *repo.Site {
	return &repo.Site{
		ID:            "site-001",
		PrimaryDomain: "example.com",
		Status:        "enabled",
		AccessLogPath: "/var/log/nginx/example.com.access.log",
		ErrorLogPath:  "/var/log/nginx/example.com.error.log",
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

func TestGet_InvalidType(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, &mockAgentCaller{})
	_, err := svc.Get(context.Background(), "site-001", "invalid", 200)
	if !isAppErr(err, app.ErrBadRequest) {
		t.Errorf("期望 BAD_REQUEST 错误，实际: %v", err)
	}
}

func TestGet_SiteNotFound(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: nil}, &mockOpRecorder{}, &mockAgentCaller{})
	_, err := svc.Get(context.Background(), "nonexistent", "access", 200)
	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND 错误，实际: %v", err)
	}
}

func TestGet_NoLogPath(t *testing.T) {
	site := testLogSite()
	site.AccessLogPath = ""
	svc := NewService(&mockSiteGetter{site: site}, &mockOpRecorder{}, &mockAgentCaller{})
	_, err := svc.Get(context.Background(), "site-001", "access", 200)
	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND（无日志路径），实际: %v", err)
	}
}

func TestGet_Success(t *testing.T) {
	agent := &mockAgentCaller{
		tailResp: &agentclient.LogTailResponse{
			Lines:     []string{"line1", "line2"},
			Truncated: false,
		},
	}
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, agent)

	resp, err := svc.Get(context.Background(), "site-001", "access", 200)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if resp.Type != "access" {
		t.Errorf("Type 期望 access，实际 %s", resp.Type)
	}
	if resp.Path != "/var/log/nginx/example.com.access.log" {
		t.Errorf("Path 不正确: %s", resp.Path)
	}
	if len(resp.Lines) != 2 {
		t.Errorf("期望 2 行，实际 %d", len(resp.Lines))
	}
	if resp.Truncated {
		t.Error("不应 truncated")
	}
}

func TestGet_ErrorLog(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, &mockAgentCaller{})

	resp, err := svc.Get(context.Background(), "site-001", "error", 50)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("Type 期望 error，实际 %s", resp.Type)
	}
	if resp.Path != "/var/log/nginx/example.com.error.log" {
		t.Errorf("Path 不正确: %s", resp.Path)
	}
}

func TestGet_DefaultLines(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, &mockAgentCaller{})
	resp, err := svc.Get(context.Background(), "site-001", "access", 0)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	_ = resp // 只是验证 lines=0 时使用默认值不报错
}

func TestGet_OverMaxLines(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, &mockAgentCaller{})
	resp, err := svc.Get(context.Background(), "site-001", "access", 5000)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	_ = resp // 验证超过上限时不报错
}

// ============================================================
// Truncate 测试
// ============================================================

func TestTruncate_InvalidType(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, &mockAgentCaller{})
	_, err := svc.Truncate(context.Background(), "site-001", &TruncateRequest{Type: "invalid", Confirm: true}, "req-001")
	if !isAppErr(err, app.ErrBadRequest) {
		t.Errorf("期望 BAD_REQUEST 错误，实际: %v", err)
	}
}

func TestTruncate_NotConfirmed(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, &mockAgentCaller{})
	_, err := svc.Truncate(context.Background(), "site-001", &TruncateRequest{Type: "access", Confirm: false}, "req-001")
	if !isAppErr(err, app.ErrValidationFailed) {
		t.Errorf("期望 VALIDATION_FAILED 错误，实际: %v", err)
	}
}

func TestTruncate_SiteNotFound(t *testing.T) {
	svc := NewService(&mockSiteGetter{site: nil}, &mockOpRecorder{}, &mockAgentCaller{})
	_, err := svc.Truncate(context.Background(), "nonexistent", &TruncateRequest{Type: "access", Confirm: true}, "req-001")
	if !isAppErr(err, app.ErrNotFound) {
		t.Errorf("期望 NOT_FOUND 错误，实际: %v", err)
	}
}

func TestTruncate_AgentFailed(t *testing.T) {
	agent := &mockAgentCaller{truncErr: context.DeadlineExceeded}
	svc := NewService(&mockSiteGetter{site: testLogSite()}, &mockOpRecorder{}, agent)

	_, err := svc.Truncate(context.Background(), "site-001", &TruncateRequest{Type: "access", Confirm: true}, "req-001")
	if !isAppErr(err, app.ErrAgentUnavailable) {
		t.Errorf("期望 AGENT_UNAVAILABLE 错误，实际: %v", err)
	}
}

func TestTruncate_Success(t *testing.T) {
	agent := &mockAgentCaller{}
	opRec := &mockOpRecorder{}
	svc := NewService(&mockSiteGetter{site: testLogSite()}, opRec, agent)

	resp, err := svc.Truncate(context.Background(), "site-001", &TruncateRequest{Type: "access", Confirm: true}, "req-001")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if !resp.Truncated {
		t.Error("Truncated 应为 true")
	}
	if resp.OperationID == "" {
		t.Error("OperationID 不应为空")
	}
	if opRec.status != "success" {
		t.Errorf("操作状态应为 success，实际: %s", opRec.status)
	}
}
