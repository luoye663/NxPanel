package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/update"
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
	checking        map[string]bool
	channels        map[string]channelCache
	channel         string
	feed            string
	candidate       *candidate
	download        *Download
	root            context.Context
	closed          bool
	wg              sync.WaitGroup
	cancel          context.CancelFunc
	startOnce       sync.Once
	closeOnce       sync.Once
}

// manualCooldown 手动触发检查的最小间隔，避免频繁调用触发 GitHub API 限流。
const manualCooldown = 60 * time.Second

type channelCache struct {
	status    *UpgradeStatus
	candidate *candidate
	checked   time.Time
}

type UpgradeStatus struct {
	HasUpgrade     bool          `json:"has_upgrade"`
	CurrentVersion string        `json:"current_version"`
	LatestVersion  string        `json:"latest_version,omitempty"`
	ReleaseURL     string        `json:"release_url,omitempty"`
	PublishedAt    string        `json:"published_at,omitempty"`
	Body           string        `json:"body,omitempty"`
	CheckedAt      string        `json:"checked_at"`
	Error          string        `json:"error,omitempty"`
	Channel        string        `json:"channel"`
	Available      bool          `json:"installable"`
	Managed        bool          `json:"managed"`
	Docker         bool          `json:"docker"`
	Task           *update.State `json:"task,omitempty"`
	Download       *Download     `json:"download,omitempty"`
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	HTMLURL     string        `json:"html_url"`
	PublishedAt string        `json:"published_at"`
	Body        string        `json:"body"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []githubAsset `json:"assets"`
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

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
		feed:     update.TestFeed(),
		channel:  "stable",
		checking: make(map[string]bool),
		channels: make(map[string]channelCache),
		httpClient: &http.Client{
			Timeout:       15 * time.Second,
			CheckRedirect: validateRedirect,
		},
		status: &UpgradeStatus{
			CurrentVersion: app.Version,
			CheckedAt:      time.Time{}.UTC().Format(time.RFC3339),
			Channel:        "stable",
		},
	}
}

func (s *Service) Start(ctx context.Context) {
	s.startOnce.Do(func() {
		ctx, s.cancel = context.WithCancel(ctx)
		s.root = ctx
		if !s.enabled {
			slog.Info("升级检测已禁用")
			return
		}
		slog.Info("升级检测服务已启动", "interval", s.interval, "repo", s.repo)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.run(ctx)
		}()
	})
}

func (s *Service) Close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		if s.cancel != nil {
			s.cancel()
		}
	})
	s.wg.Wait()
}

func (s *Service) GetStatus() *UpgradeStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := *s.status
	if s.download != nil {
		progress := *s.download
		cp.Download = &progress
	}
	return &cp
}

func (s *Service) SetChannel(channel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if channel != s.channel {
		s.channels[s.channel] = channelCache{s.status, s.candidate, s.lastManualCheck}
		s.channel = channel
		cached := s.channels[channel]
		s.status, s.candidate, s.lastManualCheck = cached.status, cached.candidate, cached.checked
		if s.status == nil {
			s.status = &UpgradeStatus{Channel: channel, CurrentVersion: app.Version}
		}
	}
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

	s.check(ctx, true)
	return s.GetStatus()
}

func (s *Service) run(ctx context.Context) {
	select {
	case <-time.After(5 * time.Minute):
	case <-ctx.Done():
		return
	}
	s.check(ctx, false)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.check(ctx, false)
		}
	}
}

func (s *Service) check(ctx context.Context, manual bool) {
	s.mu.Lock()
	channel := s.channel
	if s.checking[channel] || manual && !s.lastManualCheck.IsZero() && time.Since(s.lastManualCheck) < manualCooldown {
		s.mu.Unlock()
		return
	}
	s.checking[channel] = true
	if manual {
		s.lastManualCheck = time.Now()
	}
	s.mu.Unlock()

	result, err := s.fetchCandidate(ctx, channel)
	var install *candidate
	if err == nil && result != nil && semver.Compare(normalizeVersion(result.TagName), normalizeVersion(app.Version)) > 0 {
		install = s.verifyCandidate(ctx, result)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.checking[channel] = false
	if channel != s.channel {
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.status.CheckedAt = now

	if err != nil {
		s.candidate = nil
		s.status.Available = false
		s.status.Error = "无法获取版本信息：" + err.Error()
		return
	}
	s.candidate = install
	s.status.Available = install != nil
	if result == nil {
		s.status = &UpgradeStatus{Channel: channel, CurrentVersion: app.Version, CheckedAt: now}
		return
	}

	current := normalizeVersion(app.Version)
	latest := normalizeVersion(result.TagName)

	s.status.LatestVersion = result.TagName
	if s.feed == "" {
		s.status.ReleaseURL = "https://github.com/" + s.repo + "/releases/tag/" + result.TagName
	}
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
	if !validRepo(s.repo) {
		return nil, errors.New("无效的 GitHub 仓库配置")
	}
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
	if !update.ValidVersion(v) {
		return ""
	}
	return v
}
