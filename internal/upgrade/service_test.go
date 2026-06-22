package upgrade

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/app"
)

// newTestService 构造一个指向 mock GitHub API 的 Service。
func newTestService(t *testing.T, srv *httptest.Server, currentVersion string) *Service {
	t.Helper()
	s := NewService(app.UpgradeConfig{
		Enabled:       true,
		CheckInterval: "6h",
		GitHubRepo:    "owner/repo",
	})
	s.baseURL = srv.URL
	// 覆盖当前版本用于断言
	app.Version = currentVersion
	t.Cleanup(func() { app.Version = "0.1.0-dev" })
	return s
}

func newMockGitHubServer(t *testing.T, status int, payload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const releasePayload = `{
	"tag_name": "v9.9.9",
	"html_url": "https://github.com/owner/repo/releases/tag/v9.9.9",
	"published_at": "2026-06-01T00:00:00Z",
	"body": "release notes"
}`

// TestCheckNow_FetchesAndDetectsUpgrade 首次手动触发应向 mock 发起请求并标记有升级。
func TestCheckNow_FetchesAndDetectsUpgrade(t *testing.T) {
	srv := newMockGitHubServer(t, http.StatusOK, releasePayload)
	s := newTestService(t, srv, "v1.0.0")

	status := s.CheckNow(context.Background())

	if !status.HasUpgrade {
		t.Fatalf("应检测到升级，got status=%+v", status)
	}
	if status.LatestVersion != "v9.9.9" {
		t.Errorf("LatestVersion 期望 v9.9.9，实际 %q", status.LatestVersion)
	}
	if status.ReleaseURL != "https://github.com/owner/repo/releases/tag/v9.9.9" {
		t.Errorf("ReleaseURL 不匹配：%q", status.ReleaseURL)
	}
	if status.CheckedAt == "" || status.CheckedAt == "0001-01-01T00:00:00Z" {
		t.Errorf("CheckedAt 应被刷新，实际 %q", status.CheckedAt)
	}
}

// TestCheckNow_CooldownSkipsFetch 60 秒冷却内的重复调用不应再次发起请求。
func TestCheckNow_CooldownSkipsFetch(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(releasePayload))
	}))
	t.Cleanup(srv.Close)
	s := newTestService(t, srv, "v1.0.0")
	s.baseURL = srv.URL

	first := s.CheckNow(context.Background())
	if hits != 1 {
		t.Fatalf("首次检查应命中 mock 一次，实际 %d", hits)
	}
	firstCheckedAt := first.CheckedAt

	// 立即第二次：冷却内，不应再次请求
	time.Sleep(50 * time.Millisecond)
	second := s.CheckNow(context.Background())
	if hits != 1 {
		t.Fatalf("冷却内不应再次请求 mock，实际命中 %d", hits)
	}
	if second.CheckedAt != firstCheckedAt {
		t.Errorf("冷却内 CheckedAt 不应变：first=%q second=%q", firstCheckedAt, second.CheckedAt)
	}
}

// TestCheckNow_CooldownExpiry 冷却过期后应允许再次发起请求。
func TestCheckNow_CooldownExpiry(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(releasePayload))
	}))
	t.Cleanup(srv.Close)
	s := newTestService(t, srv, "v1.0.0")
	s.baseURL = srv.URL

	s.CheckNow(context.Background())
	if hits != 1 {
		t.Fatalf("首次检查应命中一次，实际 %d", hits)
	}

	// 手动把 lastManualCheck 回拨到冷却窗口之外
	s.mu.Lock()
	s.lastManualCheck = time.Now().Add(-manualCooldown - time.Second)
	s.mu.Unlock()

	s.CheckNow(context.Background())
	if hits != 2 {
		t.Fatalf("冷却过期后应再次请求 mock，期望命中 2 次，实际 %d", hits)
	}
}

// TestCheckNow_DisabledReturnsCached enabled=false 时直接返回缓存，不发起请求。
func TestCheckNow_DisabledReturnsCached(t *testing.T) {
	srv := newMockGitHubServer(t, http.StatusOK, releasePayload)
	s := NewService(app.UpgradeConfig{Enabled: false, GitHubRepo: "owner/repo"})
	s.baseURL = srv.URL

	status := s.CheckNow(context.Background())
	if status.HasUpgrade {
		t.Errorf("禁用状态下不应检测到升级")
	}
	if status.CurrentVersion == "" {
		t.Errorf("禁用状态下仍应返回 current_version")
	}
}

// TestCheckNow_Non200SetsError 非 200 响应应写入带状态码的错误且不更新版本字段。
func TestCheckNow_Non200SetsError(t *testing.T) {
	srv := newMockGitHubServer(t, http.StatusNotFound, `{"message":"Not Found","documentation_url":"https://docs.github.com"}`)
	s := newTestService(t, srv, "v1.0.0")

	status := s.CheckNow(context.Background())
	if status.HasUpgrade {
		t.Errorf("失败时不应标记有升级")
	}
	if status.Error == "" {
		t.Errorf("失败时应写入错误信息")
	}
	if !strings.Contains(status.Error, "404") {
		t.Errorf("错误信息应包含 HTTP 状态码 404，实际 %q", status.Error)
	}
	if !strings.Contains(status.Error, "Not Found") {
		t.Errorf("错误信息应包含 GitHub message，实际 %q", status.Error)
	}
	if !strings.Contains(status.Error, "Release") {
		t.Errorf("404 应附带 Tag≠Release 提示，实际 %q", status.Error)
	}
	if status.LatestVersion != "" {
		t.Errorf("失败时 LatestVersion 应为空，实际 %q", status.LatestVersion)
	}
}

// TestCheckNow_ForbiddenSetsRateLimitHint 403 应附带速率限制提示。
func TestCheckNow_ForbiddenSetsRateLimitHint(t *testing.T) {
	srv := newMockGitHubServer(t, http.StatusForbidden, `{"message":"API rate limit exceeded"}`)
	s := newTestService(t, srv, "v1.0.0")

	status := s.CheckNow(context.Background())
	if !strings.Contains(status.Error, "403") {
		t.Errorf("应包含 403，实际 %q", status.Error)
	}
	if !strings.Contains(status.Error, "速率限制") {
		t.Errorf("403 应附带速率限制提示，实际 %q", status.Error)
	}
}

// TestGetStatus_InitiallyZero 新建 Service 的初始状态应包含当前版本与零时刻。
func TestGetStatus_InitiallyZero(t *testing.T) {
	app.Version = "v1.2.3"
	t.Cleanup(func() { app.Version = "0.1.0-dev" })
	s := NewService(app.UpgradeConfig{Enabled: true, GitHubRepo: "owner/repo"})

	status := s.GetStatus()
	if status.CurrentVersion != "v1.2.3" {
		t.Errorf("初始 CurrentVersion 期望 v1.2.3，实际 %q", status.CurrentVersion)
	}
	if status.HasUpgrade {
		t.Errorf("初始状态不应有升级")
	}
}

// TestNormalizeVersion 确保版本归一化符合预期。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.2.3", "v1.2.3"},
		{"v1.2.3", "v1.2.3"},
		{"v1.2.3-rc1", "v1.2.3"},
		{"not-a-version", ""},
		{"v2", "v2"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 防止 githubRelease 字段被意外修改导致 JSON 解析失败。
func TestGithubReleaseParsing(t *testing.T) {
	var r githubRelease
	if err := json.Unmarshal([]byte(releasePayload), &r); err != nil {
		t.Fatalf("解析失败：%v", err)
	}
	if r.TagName != "v9.9.9" || r.HTMLURL == "" || r.Body == "" {
		t.Errorf("解析结果不完整：%+v", r)
	}
}
