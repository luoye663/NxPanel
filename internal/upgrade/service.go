package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/luoye663/nxpanel/internal/app"
)

type Service struct {
	enabled    bool
	repo       string
	baseURL    string
	interval   time.Duration
	httpClient *http.Client

	mu              sync.RWMutex
	status          *UpgradeStatus
	lastManualCheck time.Time
}

// manualCooldown 手动触发检查的最小间隔，避免频繁调用触发 GitHub API 限流。
const manualCooldown = 60 * time.Second

type UpgradeStatus struct {
	HasUpgrade     bool   `json:"has_upgrade"`
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	ReleaseURL     string `json:"release_url,omitempty"`
	PublishedAt    string `json:"published_at,omitempty"`
	Body           string `json:"body,omitempty"`
	CheckedAt      string `json:"checked_at"`
	Error          string `json:"error,omitempty"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
}

func NewService(cfg app.UpgradeConfig) *Service {
	interval := app.ParseDurationOrDefault(cfg.CheckInterval, 6*time.Hour)
	if interval < 1*time.Hour {
		interval = 1 * time.Hour
	}
	return &Service{
		enabled:  cfg.Enabled,
		repo:     cfg.GitHubRepo,
		baseURL:  "https://api.github.com",
		interval: interval,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		status: &UpgradeStatus{
			CurrentVersion: app.Version,
			CheckedAt:      time.Time{}.UTC().Format(time.RFC3339),
		},
	}
}

func (s *Service) Start(ctx context.Context) {
	if !s.enabled {
		slog.Info("升级检测已禁用")
		return
	}
	slog.Info("升级检测服务已启动", "interval", s.interval, "repo", s.repo)
	go s.run(ctx)
}

func (s *Service) GetStatus() *UpgradeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := *s.status
	return &cp
}

// CheckNow 手动触发一次升级检查。
//
// 行为：
//   - enabled=false 时直接返回当前内存快照，不发起网络请求
//   - 距上次手动检查不足 manualCooldown 时，直接返回当前快照（冷却防抖）
//   - 否则同步调用 check() 向 GitHub 发起一次请求并刷新内存状态
//
// 该方法独立于后台 run() 的调度（5 分钟首延 + interval 周期），
// 不会重置后台 ticker，仅记录手动检查时间用于冷却判定。
func (s *Service) CheckNow(ctx context.Context) *UpgradeStatus {
	if !s.enabled {
		return s.GetStatus()
	}

	s.mu.RLock()
	cooldown := !s.lastManualCheck.IsZero() && time.Since(s.lastManualCheck) < manualCooldown
	s.mu.RUnlock()
	if cooldown {
		return s.GetStatus()
	}

	s.check(ctx)

	s.mu.Lock()
	s.lastManualCheck = time.Now()
	s.mu.Unlock()

	return s.GetStatus()
}

func (s *Service) run(ctx context.Context) {
	select {
	case <-time.After(5 * time.Minute):
	case <-ctx.Done():
		return
	}
	s.check(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

func (s *Service) check(ctx context.Context) {
	result, err := s.fetchLatest(ctx)

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	s.status.CheckedAt = now

	if err != nil {
		s.status.Error = "无法获取版本信息：" + err.Error()
		return
	}

	current := normalizeVersion(app.Version)
	latest := normalizeVersion(result.TagName)

	s.status.LatestVersion = result.TagName
	s.status.ReleaseURL = result.HTMLURL
	s.status.PublishedAt = result.PublishedAt
	s.status.Body = result.Body
	s.status.CurrentVersion = app.Version
	s.status.Error = ""

	if current != "" && latest != "" && semver.Compare(latest, current) > 0 {
		s.status.HasUpgrade = true
		slog.Info("发现新版本", "current", app.Version, "latest", result.TagName)
	} else {
		s.status.HasUpgrade = false
	}
}

func (s *Service) fetchLatest(ctx context.Context) (*githubRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", s.baseURL, s.repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		slog.Debug("创建 GitHub API 请求失败", "error", err)
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		slog.Debug("请求 GitHub Releases API 失败", "error", err)
		return nil, fmt.Errorf("网络错误: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("GitHub Releases API 返回非 200", "status", resp.StatusCode, "repo", s.repo)
		msg := fmt.Sprintf("GitHub 返回 HTTP %d", resp.StatusCode)
		switch resp.StatusCode {
		case http.StatusNotFound:
			// releases/latest 只认已发布的 Release；仅有 Tag 会返回 404。
			msg += "（仓库不存在，或仅有 Tag 未发布 Release；请到 GitHub Releases 页面正式发布）"
		case http.StatusForbidden:
			msg += "（可能触发 GitHub 未认证 API 速率限制，60 次/小时/IP）"
		}
		if hint := parseGitHubMessage(resp.Body); hint != "" {
			msg += "：" + hint
		}
		return nil, errors.New(msg)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		slog.Debug("读取 GitHub API 响应失败", "error", err)
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		slog.Debug("解析 GitHub API 响应失败", "error", err)
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return &release, nil
}

// parseGitHubMessage 从 GitHub 错误响应体中提取 message 字段（如 "Not Found"）。
// 读取量限制在 4KB，足够覆盖错误响应；用于辅助诊断。
func parseGitHubMessage(r io.Reader) string {
	body, err := io.ReadAll(io.LimitReader(r, 4<<10))
	if err != nil {
		return ""
	}
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &m) == nil {
		return m.Message
	}
	return ""
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	idx := strings.Index(v, "-")
	if idx > 0 {
		v = v[:idx]
	}
	if semver.Canonical(v) == "" {
		return ""
	}
	return v
}
