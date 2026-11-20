package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/update"
)

const listLimit = 4 << 20

type githubAsset struct {
	Name string `json:"name"`
}

type candidate struct {
	version   string
	manifest  []byte
	signature []byte
	asset     update.Asset
	url       string
}

type Download struct {
	Version  string `json:"version"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Error    string `json:"error,omitempty"`
}

func (s *Service) fetchCandidate(ctx context.Context, channel string) (*githubRelease, error) {
	if s.feed == "" && channel == "stable" {
		release, err := s.fetchLatest(ctx)
		if err == nil && !eligible(*release, channel) {
			return nil, errors.New("GitHub 未返回有效的稳定版")
		}
		return release, err
	}
	var best *githubRelease
	for page := 1; page <= 4; page++ {
		source := s.feed
		if source == "" {
			if !validRepo(s.repo) {
				return nil, errors.New("无效的 GitHub 仓库配置")
			}
			source = fmt.Sprintf("%s/repos/%s/releases?per_page=30&page=%d", s.baseURL, s.repo, page)
		} else if _, err := testURL(source); err != nil {
			return nil, err
		}
		data, err := fetchBytes(ctx, s.httpClient, source, listLimit)
		if err != nil {
			return nil, err
		}
		var releases []githubRelease
		if err := json.Unmarshal(data, &releases); err != nil {
			return nil, err
		}
		for _, release := range releases {
			if eligible(release, channel) && (best == nil || semver.Compare(release.TagName, best.TagName) > 0) {
				selected := release
				best = &selected
			}
		}
		if s.feed != "" || len(releases) < 30 {
			break
		}
	}
	return best, nil
}

func eligible(release githubRelease, channel string) bool {
	return !release.Draft && update.ValidVersion(release.TagName) && release.Prerelease == (update.Channel(release.TagName) == "preview") && (channel == "preview" || !release.Prerelease)
}

func validRepo(repo string) bool {
	if !repoName.MatchString(repo) {
		return false
	}
	for _, part := range strings.Split(repo, "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (s *Service) assetURL(version, name string) string {
	if s.feed == "" {
		return "https://github.com/" + s.repo + "/releases/download/" + version + "/" + name
	}
	base, err := testURL(s.feed)
	if err != nil {
		return ""
	}
	base.Path, base.RawPath = path.Join(path.Dir(base.Path), version, name), ""
	return base.String()
}

func testURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid development update index URL")
	}
	return parsed, nil
}

func validateRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 5 || req.URL.User != nil {
		return errors.New("invalid update redirect")
	}
	if feed := update.TestFeed(); feed != "" {
		base, err := testURL(feed)
		if err == nil && base.Scheme == req.URL.Scheme && base.Host == req.URL.Host {
			return nil
		}
		return errors.New("development update redirect left the configured origin")
	}
	if req.URL.Scheme == "https" && req.URL.Port() == "" {
		switch req.URL.Hostname() {
		case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			return nil
		}
	}
	return errors.New("update redirect left trusted GitHub origins")
}

func fetchBytes(ctx context.Context, client *http.Client, source string, max int64) ([]byte, error) {
	response, err := fetch(ctx, client, source, max)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err == nil && int64(len(data)) > max {
		err = errors.New("update response exceeds size limit")
	}
	return data, err
}

func fetch(ctx context.Context, client *http.Client, source string, max int64) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > max {
		response.Body.Close()
		return nil, fmt.Errorf("更新源返回 HTTP %d 或响应超出大小限制", response.StatusCode)
	}
	return response, nil
}

func (s *Service) verifyCandidate(ctx context.Context, release *githubRelease) *candidate {
	if !update.ValidVersion(app.Version) {
		return nil
	}
	keys, err := update.Keys(update.ActiveKeys())
	if err != nil || len(keys) == 0 {
		return nil
	}
	if s.feed == "" {
		var manifest, signature bool
		for _, asset := range release.Assets {
			manifest = manifest || asset.Name == update.ManifestName
			signature = signature || asset.Name == update.SignatureName
		}
		if !manifest || !signature {
			return nil
		}
	}
	raw, err := fetchBytes(ctx, s.httpClient, s.assetURL(release.TagName, update.ManifestName), update.MetadataMax)
	if err != nil {
		return nil
	}
	signature, err := fetchBytes(ctx, s.httpClient, s.assetURL(release.TagName, update.SignatureName), update.MetadataMax)
	if err != nil {
		return nil
	}
	manifest, asset, err := update.Verify(raw, signature, keys)
	if err != nil || manifest.Version != release.TagName {
		return nil
	}
	return &candidate{version: manifest.Version, manifest: raw, signature: signature, asset: asset, url: s.assetURL(manifest.Version, asset.Name)}
}

func (s *Service) Prepare(version string, updater *update.Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.root == nil || !s.enabled || s.candidate == nil || s.candidate.version != version || updater == nil {
		return errors.New("没有可安装的已验证候选版本，请重新检查")
	}
	if s.download != nil && s.download.Error == "" {
		return errors.New("已有下载任务")
	}
	job := *s.candidate
	s.download = &Download{Version: version, Total: job.asset.Size}
	s.wg.Add(1)
	go s.prepare(job, updater)
	return nil
}

func (s *Service) prepare(job candidate, updater *update.Client) {
	defer s.wg.Done()
	ctx, cancel := context.WithTimeout(s.root, 15*time.Minute)
	defer cancel()
	err := s.downloadPackage(ctx, job, updater)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.download.Error = "下载或校验失败：" + err.Error()
		return
	}
	s.download = nil
}

func (s *Service) downloadPackage(ctx context.Context, job candidate, updater *update.Client) error {
	state, err := updater.Status(ctx)
	if err != nil {
		return err
	}
	if state.Busy() || state.Phase == update.Prepared {
		return errors.New("请先完成或放弃当前更新")
	}
	client := *s.httpClient
	client.Timeout = 14 * time.Minute
	response, err := fetch(ctx, &client, job.url, job.asset.Size)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	file, err := os.CreateTemp("", "nxpanel-update-*")
	if err != nil {
		return err
	}
	defer file.Close()
	defer os.Remove(file.Name())
	if err := os.Remove(file.Name()); err != nil {
		return err
	}
	if err := update.CopyPackage(file, &progressReader{source: response.Body, service: s}, job.asset); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = updater.Stage(ctx, job.manifest, job.signature, file)
	return err
}

type progressReader struct {
	source  io.Reader
	service *Service
}

func (reader *progressReader) Read(buffer []byte) (int, error) {
	count, err := reader.source.Read(buffer)
	reader.service.mu.Lock()
	reader.service.download.Received += int64(count)
	reader.service.mu.Unlock()
	return count, err
}
